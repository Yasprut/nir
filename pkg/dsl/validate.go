package dsl

import "fmt"

// Validate проверяет синтаксис DSL-выражения без выполнения.
func Validate(expr string) error {
	tokens, err := tokenize(expr)
	if err != nil {
		return fmt.Errorf("tokenize: %w", err)
	}
	if len(tokens) == 0 {
		return fmt.Errorf("empty expression")
	}
	p := &parser{tokens: tokens}
	_, err = p.parseExpr()
	if err != nil {
		return fmt.Errorf("parse: %w", err)
	}
	if p.pos < len(p.tokens) {
		return fmt.Errorf("unexpected token at position %d: %s", p.pos, p.tokens[p.pos].value)
	}
	return nil
}
