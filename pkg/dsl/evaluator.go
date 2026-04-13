package dsl

import (
	"fmt"
	"strings"
	"unicode"

	pb "nir/proto/iam/v1"
)

// CONTEXT

// EvalContext содержит все данные, доступные DSL-выражениям.
type EvalContext struct {
	Request *pb.AccessRequest
	HR      *pb.HRResponse // nil если HR ещё не вызван
}

// Evaluate парсит и вычисляет DSL-выражение.

// Поддерживаемый синтаксис:
//
//	resource.type == "app"
//	subject.department != "hr"
//	"risk-team" IN subject.groups
//	resource.type == "app" AND environment == "PROD"
//	subject.department == "finance" OR "risk-team" IN subject.groups
//	resource.type == "app" AND (subject.department == "finance" OR "vip" IN resource.labels)
//	NOT subject.department == "hr"
//	NOT ("blocked" IN resource.labels)
func Evaluate(expr string, ctx EvalContext) (bool, error) {
	tokens, err := tokenize(expr)
	if err != nil {
		return false, fmt.Errorf("tokenize: %w", err)
	}
	p := &parser{tokens: tokens}
	result, err := p.parseExpr()
	if err != nil {
		return false, fmt.Errorf("parse: %w", err)
	}
	if p.pos < len(p.tokens) {
		return false, fmt.Errorf("unexpected token at position %d: %s", p.pos, p.tokens[p.pos].value)
	}
	return result.eval(ctx)
}

// TOKENS

type tokenType int

const (
	tokString tokenType = iota // "quoted string"
	tokIdent                   // field.path
	tokEQ                      // ==
	tokNEQ                     // !=
	tokAND                     // AND
	tokOR                      // OR
	tokIN                      // IN
	tokNOT                     // NOT
	tokLParen                  // (
	tokRParen                  // )
)

type token struct {
	typ   tokenType
	value string
}

func tokenize(input string) ([]token, error) {
	var tokens []token
	runes := []rune(input)
	i := 0

	for i < len(runes) {
		ch := runes[i]

		if unicode.IsSpace(ch) {
			i++
			continue
		}

		// Строка в кавычках
		if ch == '"' {
			j := i + 1
			for j < len(runes) && runes[j] != '"' {
				if runes[j] == '\\' {
					j++
				}
				j++
			}
			if j >= len(runes) {
				return nil, fmt.Errorf("unterminated string at position %d", i)
			}
			tokens = append(tokens, token{tokString, string(runes[i+1 : j])})
			i = j + 1
			continue
		}

		// ==
		if ch == '=' && i+1 < len(runes) && runes[i+1] == '=' {
			tokens = append(tokens, token{tokEQ, "=="})
			i += 2
			continue
		}
		// !=
		if ch == '!' && i+1 < len(runes) && runes[i+1] == '=' {
			tokens = append(tokens, token{tokNEQ, "!="})
			i += 2
			continue
		}

		if ch == '(' {
			tokens = append(tokens, token{tokLParen, "("})
			i++
			continue
		}
		if ch == ')' {
			tokens = append(tokens, token{tokRParen, ")"})
			i++
			continue
		}

		// Идентификатор или ключевое слово
		if unicode.IsLetter(ch) || ch == '_' {
			j := i
			for j < len(runes) && (unicode.IsLetter(runes[j]) || unicode.IsDigit(runes[j]) || runes[j] == '_' || runes[j] == '.') {
				j++
			}
			word := string(runes[i:j])
			switch strings.ToUpper(word) {
			case "AND":
				tokens = append(tokens, token{tokAND, "AND"})
			case "OR":
				tokens = append(tokens, token{tokOR, "OR"})
			case "IN":
				tokens = append(tokens, token{tokIN, "IN"})
			case "NOT":
				tokens = append(tokens, token{tokNOT, "NOT"})
			default:
				tokens = append(tokens, token{tokIdent, word})
			}
			i = j
			continue
		}

		return nil, fmt.Errorf("unexpected character '%c' at position %d", ch, i)
	}

	return tokens, nil
}

// AST

type node interface {
	eval(ctx EvalContext) (bool, error)
}

type andNode struct{ left, right node }

func (n *andNode) eval(ctx EvalContext) (bool, error) {
	l, err := n.left.eval(ctx)
	if err != nil || !l {
		return false, err
	}
	return n.right.eval(ctx)
}

type orNode struct{ left, right node }

func (n *orNode) eval(ctx EvalContext) (bool, error) {
	l, err := n.left.eval(ctx)
	if err != nil {
		return false, err
	}
	if l {
		return true, nil
	}
	return n.right.eval(ctx)
}

type notNode struct{ inner node }

func (n *notNode) eval(ctx EvalContext) (bool, error) {
	v, err := n.inner.eval(ctx)
	if err != nil {
		return false, err
	}
	return !v, nil
}

type compareNode struct {
	left  string
	op    tokenType
	right string
}

func (n *compareNode) eval(ctx EvalContext) (bool, error) {
	l := resolveValue(n.left, ctx)
	r := resolveValue(n.right, ctx)
	switch n.op {
	case tokEQ:
		return strings.EqualFold(l, r), nil
	case tokNEQ:
		return !strings.EqualFold(l, r), nil
	}
	return false, fmt.Errorf("unknown comparison operator")
}

type inNode struct {
	value string // что ищем (литерал или путь)
	field string // путь к массиву
}

func (n *inNode) eval(ctx EvalContext) (bool, error) {
	arr := resolveArray(n.field, ctx)
	needle := resolveValue(n.value, ctx)
	for _, item := range arr {
		if strings.EqualFold(item, needle) {
			return true, nil
		}
	}
	return false, nil
}

// PARSER
// Грамматика (приоритет снизу вверх):
//   expr     → orExpr
//   orExpr   → andExpr ("OR" andExpr)*
//   andExpr  → unary   ("AND" unary)*
//   unary    → "NOT" unary | primary
//   primary  → "(" expr ")" | value ("==" | "!=") value | value "IN" ident

type parser struct {
	tokens []token
	pos    int
}

func (p *parser) peek() *token {
	if p.pos < len(p.tokens) {
		return &p.tokens[p.pos]
	}
	return nil
}

func (p *parser) advance() token {
	t := p.tokens[p.pos]
	p.pos++
	return t
}

func (p *parser) expect(typ tokenType) (token, error) {
	t := p.peek()
	if t == nil {
		return token{}, fmt.Errorf("unexpected end of expression")
	}
	if t.typ != typ {
		return token{}, fmt.Errorf("expected %d, got %q", typ, t.value)
	}
	return p.advance(), nil
}

func (p *parser) parseExpr() (node, error) {
	return p.parseOr()
}

func (p *parser) parseOr() (node, error) {
	left, err := p.parseAnd()
	if err != nil {
		return nil, err
	}
	for p.peek() != nil && p.peek().typ == tokOR {
		p.advance()
		right, err := p.parseAnd()
		if err != nil {
			return nil, err
		}
		left = &orNode{left, right}
	}
	return left, nil
}

func (p *parser) parseAnd() (node, error) {
	left, err := p.parseUnary()
	if err != nil {
		return nil, err
	}
	for p.peek() != nil && p.peek().typ == tokAND {
		p.advance()
		right, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		left = &andNode{left, right}
	}
	return left, nil
}

func (p *parser) parseUnary() (node, error) {
	if p.peek() != nil && p.peek().typ == tokNOT {
		p.advance()
		inner, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		return &notNode{inner}, nil
	}
	return p.parsePrimary()
}

func (p *parser) parsePrimary() (node, error) {
	t := p.peek()
	if t == nil {
		return nil, fmt.Errorf("unexpected end of expression")
	}

	// ( expr )
	if t.typ == tokLParen {
		p.advance()
		inner, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(tokRParen); err != nil {
			return nil, fmt.Errorf("missing closing parenthesis")
		}
		return inner, nil
	}

	// Должен быть string или ident
	if t.typ != tokString && t.typ != tokIdent {
		return nil, fmt.Errorf("unexpected token: %q", t.value)
	}

	left := p.advance()

	next := p.peek()
	if next == nil {
		return nil, fmt.Errorf("unexpected end after %q", left.value)
	}

	switch next.typ {
	case tokEQ, tokNEQ:
		op := p.advance()
		rt := p.peek()
		if rt == nil || (rt.typ != tokString && rt.typ != tokIdent) {
			return nil, fmt.Errorf("expected value after %s", op.value)
		}
		right := p.advance()
		return &compareNode{left: left.value, op: op.typ, right: right.value}, nil

	case tokIN:
		p.advance()
		ft := p.peek()
		if ft == nil || ft.typ != tokIdent {
			return nil, fmt.Errorf("expected field path after IN")
		}
		field := p.advance()
		return &inNode{value: left.value, field: field.value}, nil

	default:
		return nil, fmt.Errorf("expected ==, != or IN after %q, got %q", left.value, next.value)
	}
}

// FIELD RESOLUTION

func resolveValue(val string, ctx EvalContext) string {
	if resolved := resolveField(val, ctx); resolved != "" {
		return resolved
	}
	return val
}

func resolveField(path string, ctx EvalContext) string {
	req := ctx.Request
	hr := ctx.HR

	switch path {
	// Subject
	case "subject.user_id":
		if req != nil && req.Subject != nil {
			return req.Subject.UserId
		}
	case "subject.justification":
		if req != nil && req.Subject != nil {
			return req.Subject.Justification
		}

	// Resource
	case "resource.type":
		if req != nil && req.Resource != nil {
			return req.Resource.Type
		}
	case "resource.name":
		if req != nil && req.Resource != nil {
			return req.Resource.Name
		}
	case "resource.id":
		if req != nil && req.Resource != nil {
			return req.Resource.Id
		}
	case "environment":
		if req != nil && req.Resource != nil {
			return req.Resource.Environment.String()
		}

	// Delegation
	case "request.requested_for_user_id":
		if req != nil {
			return req.RequestedForUserId
		}
	case "request.requested_by_user_id":
		if req != nil {
			return req.RequestedByUserId
		}

	// HR fields
	case "hr.department", "subject.department":
		if hr != nil {
			return hr.Department
		}
	case "hr.position":
		if hr != nil {
			return hr.Position
		}
	case "hr.status":
		if hr != nil {
			return hr.Status
		}
	case "hr.manager_id":
		if hr != nil {
			return hr.ManagerId
		}
	case "hr.hr_bp":
		if hr != nil {
			return hr.HrBp
		}
	case "hr.full_name":
		if hr != nil {
			return hr.FullName
		}
	}

	// resource.attributes.X — динамический доступ к map
	if strings.HasPrefix(path, "resource.attributes.") {
		key := strings.TrimPrefix(path, "resource.attributes.")
		if req != nil && req.Resource != nil && req.Resource.Attributes != nil {
			return req.Resource.Attributes[key]
		}
		return ""
	}

	// subject.metadata.X
	if strings.HasPrefix(path, "subject.metadata.") {
		key := strings.TrimPrefix(path, "subject.metadata.")
		if req != nil && req.Subject != nil && req.Subject.Metadata != nil {
			return req.Subject.Metadata[key]
		}
		return ""
	}

	// hr.attributes.X
	if strings.HasPrefix(path, "hr.attributes.") {
		key := strings.TrimPrefix(path, "hr.attributes.")
		if hr != nil && hr.Attributes != nil {
			return hr.Attributes[key]
		}
		return ""
	}

	return ""
}

func resolveArray(path string, ctx EvalContext) []string {
	req := ctx.Request
	hr := ctx.HR

	switch path {
	case "subject.groups", "hr.groups":
		if hr != nil {
			return hr.Groups
		}
	case "resource.labels":
		if req != nil && req.Resource != nil {
			return req.Resource.Labels
		}
	case "subject.roles":
		if req != nil {
			var names []string
			for _, r := range req.Roles {
				names = append(names, r.Name)
			}
			return names
		}
	}

	return nil
}
