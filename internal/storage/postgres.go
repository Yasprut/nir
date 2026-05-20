package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"nir/internal/policy"

	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresStore struct {
	pool *pgxpool.Pool
}

func NewPostgresStore(databaseURL string) (*PostgresStore, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("connect to database: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		return nil, fmt.Errorf("ping database: %w", err)
	}

	return &PostgresStore{pool: pool}, nil
}

func (s *PostgresStore) Close() {
	s.pool.Close()
}

// Policy engine load (active only)

func (s *PostgresStore) LoadPolicies(ctx context.Context) ([]policy.Policy, error) {
	const query = `
		SELECT policy_id, type, priority, selectors, steps,
		       COALESCE(conditional_steps, '[]'::jsonb)
		FROM policies
		WHERE enabled = TRUE AND status = 'active'
		ORDER BY priority DESC
	`

	rows, err := s.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query policies: %w", err)
	}
	defer rows.Close()

	var policies []policy.Policy

	for rows.Next() {
		var (
			id              string
			pType           string
			priority        int
			selectorsJSON   []byte
			stepsJSON       []byte
			conditionalJSON []byte
		)

		if err := rows.Scan(&id, &pType, &priority, &selectorsJSON, &stepsJSON, &conditionalJSON); err != nil {
			return nil, fmt.Errorf("scan row: %w", err)
		}

		var selectors policy.Selectors
		if err := json.Unmarshal(selectorsJSON, &selectors); err != nil {
			return nil, fmt.Errorf("unmarshal selectors [%s]: %w", id, err)
		}

		var steps []policy.Step
		if err := json.Unmarshal(stepsJSON, &steps); err != nil {
			return nil, fmt.Errorf("unmarshal steps [%s]: %w", id, err)
		}

		var conditionalSteps []policy.ConditionalStep
		if err := json.Unmarshal(conditionalJSON, &conditionalSteps); err != nil {
			return nil, fmt.Errorf("unmarshal conditional_steps [%s]: %w", id, err)
		}

		policies = append(policies, policy.Policy{
			ID:               id,
			Type:             pType,
			Priority:         priority,
			Selectors:        selectors,
			Steps:            steps,
			ConditionalSteps: conditionalSteps,
		})
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate rows: %w", err)
	}

	return policies, nil
}

// Policy CRUD types

type PolicyRow struct {
	ID               int64           `json:"id"`
	PolicyID         string          `json:"policy_id"`
	Type             string          `json:"type"`
	Priority         int             `json:"priority"`
	Selectors        json.RawMessage `json:"selectors"`
	Steps            json.RawMessage `json:"steps"`
	ConditionalSteps json.RawMessage `json:"conditional_steps"`
	Enabled          bool            `json:"enabled"`
	Status           string          `json:"status"`
	SubmittedBy      string          `json:"submitted_by,omitempty"`
	ReviewedBy       string          `json:"reviewed_by,omitempty"`
	ReviewComment    string          `json:"review_comment,omitempty"`
	SubmittedAt      *time.Time      `json:"submitted_at,omitempty"`
}

const policyCols = `id, policy_id, type, priority, selectors, steps,
	COALESCE(conditional_steps, '[]'::jsonb), enabled,
	COALESCE(status,'active'), COALESCE(submitted_by,''), COALESCE(reviewed_by,''),
	COALESCE(review_comment,''), submitted_at`

func scanPolicyRow(row interface {
	Scan(...any) error
}, r *PolicyRow) error {
	return row.Scan(
		&r.ID, &r.PolicyID, &r.Type, &r.Priority,
		&r.Selectors, &r.Steps, &r.ConditionalSteps, &r.Enabled,
		&r.Status, &r.SubmittedBy, &r.ReviewedBy, &r.ReviewComment, &r.SubmittedAt,
	)
}

// Policy queries

func (s *PostgresStore) CreatePolicy(ctx context.Context, row PolicyRow) error {
	condSteps := row.ConditionalSteps
	if condSteps == nil {
		condSteps = []byte("[]")
	}
	status := row.Status
	if status == "" {
		status = "active"
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO policies
		  (policy_id, type, priority, selectors, steps, conditional_steps, enabled, status, submitted_by, submitted_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,NOW())
	`, row.PolicyID, row.Type, row.Priority, row.Selectors, row.Steps, condSteps,
		row.Enabled, status, row.SubmittedBy)
	return err
}

func (s *PostgresStore) UpdatePolicy(ctx context.Context, row PolicyRow) error {
	condSteps := row.ConditionalSteps
	if condSteps == nil {
		condSteps = []byte("[]")
	}
	tag, err := s.pool.Exec(ctx, `
		UPDATE policies
		SET type=$2, priority=$3, selectors=$4, steps=$5, conditional_steps=$6, enabled=$7
		WHERE policy_id=$1
	`, row.PolicyID, row.Type, row.Priority, row.Selectors, row.Steps, condSteps, row.Enabled)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("policy %s not found", row.PolicyID)
	}
	return nil
}

func (s *PostgresStore) DeletePolicy(ctx context.Context, policyID string) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM policies WHERE policy_id=$1`, policyID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("policy %s not found", policyID)
	}
	return nil
}

func (s *PostgresStore) DisablePolicy(ctx context.Context, policyID string) error {
	_, err := s.pool.Exec(ctx, `UPDATE policies SET enabled=FALSE WHERE policy_id=$1`, policyID)
	return err
}

func (s *PostgresStore) TogglePolicy(ctx context.Context, policyID string, enabled bool) error {
	tag, err := s.pool.Exec(ctx, `UPDATE policies SET enabled=$2 WHERE policy_id=$1`, policyID, enabled)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("policy %s not found", policyID)
	}
	return nil
}

func (s *PostgresStore) ListAllPolicies(ctx context.Context) ([]PolicyRow, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+policyCols+`
		FROM policies
		WHERE status = 'active'
		ORDER BY priority DESC, policy_id ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("query all policies: %w", err)
	}
	defer rows.Close()

	var result []PolicyRow
	for rows.Next() {
		var row PolicyRow
		if err := scanPolicyRow(rows, &row); err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}
		result = append(result, row)
	}
	return result, rows.Err()
}

func (s *PostgresStore) GetPolicy(ctx context.Context, policyID string) (*PolicyRow, error) {
	var row PolicyRow
	err := scanPolicyRow(s.pool.QueryRow(ctx, `
		SELECT `+policyCols+`
		FROM policies WHERE policy_id=$1
	`, policyID), &row)
	if err != nil {
		return nil, fmt.Errorf("get policy %s: %w", policyID, err)
	}
	return &row, nil
}

// Review workflow

func (s *PostgresStore) ListPendingPolicies(ctx context.Context) ([]PolicyRow, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+policyCols+`
		FROM policies
		WHERE status = 'pending_review'
		ORDER BY submitted_at ASC NULLS LAST
	`)
	if err != nil {
		return nil, fmt.Errorf("query pending policies: %w", err)
	}
	defer rows.Close()

	var result []PolicyRow
	for rows.Next() {
		var row PolicyRow
		if err := scanPolicyRow(rows, &row); err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}
		result = append(result, row)
	}
	return result, rows.Err()
}

func (s *PostgresStore) ApprovePolicy(ctx context.Context, policyID, reviewedBy string) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE policies
		SET status='active', reviewed_by=$2, review_comment=''
		WHERE policy_id=$1 AND status='pending_review'
	`, policyID, reviewedBy)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("policy %s not found or not pending", policyID)
	}
	return nil
}

func (s *PostgresStore) RejectPolicy(ctx context.Context, policyID, reviewedBy, comment string) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE policies
		SET status='rejected', reviewed_by=$2, review_comment=$3
		WHERE policy_id=$1 AND status='pending_review'
	`, policyID, reviewedBy, comment)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("policy %s not found or not pending", policyID)
	}
	return nil
}

// Users

type UserRow struct {
	ID        int       `json:"id"`
	Username  string    `json:"username"`
	Role      string    `json:"role"`
	CreatedAt time.Time `json:"created_at"`
}

func (s *PostgresStore) GetUserByUsername(ctx context.Context, username string) (*UserRow, string, error) {
	var u UserRow
	var hash string
	err := s.pool.QueryRow(ctx, `
		SELECT id, username, role, created_at, password_hash
		FROM users WHERE username=$1
	`, username).Scan(&u.ID, &u.Username, &u.Role, &u.CreatedAt, &hash)
	if err != nil {
		return nil, "", fmt.Errorf("get user: %w", err)
	}
	return &u, hash, nil
}

func (s *PostgresStore) GetUserByID(ctx context.Context, id int) (*UserRow, error) {
	var u UserRow
	err := s.pool.QueryRow(ctx, `
		SELECT id, username, role, created_at FROM users WHERE id=$1
	`, id).Scan(&u.ID, &u.Username, &u.Role, &u.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("get user by id: %w", err)
	}
	return &u, nil
}

func (s *PostgresStore) ListUsers(ctx context.Context) ([]UserRow, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, username, role, created_at FROM users ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []UserRow
	for rows.Next() {
		var u UserRow
		if err := rows.Scan(&u.ID, &u.Username, &u.Role, &u.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, u)
	}
	return result, rows.Err()
}

func (s *PostgresStore) CreateUser(ctx context.Context, username, passwordHash, role string) (*UserRow, error) {
	var u UserRow
	err := s.pool.QueryRow(ctx, `
		INSERT INTO users (username, password_hash, role)
		VALUES ($1,$2,$3)
		RETURNING id, username, role, created_at
	`, username, passwordHash, role).Scan(&u.ID, &u.Username, &u.Role, &u.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("create user: %w", err)
	}
	return &u, nil
}

func (s *PostgresStore) DeleteUser(ctx context.Context, id int) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM users WHERE id=$1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("user %d not found", id)
	}
	return nil
}

func (s *PostgresStore) UpdateUserPassword(ctx context.Context, id int, hash string) error {
	tag, err := s.pool.Exec(ctx, `UPDATE users SET password_hash=$2 WHERE id=$1`, id, hash)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("user %d not found", id)
	}
	return nil
}

// Sessions

type SessionRow struct {
	Token     string
	UserID    int
	ExpiresAt time.Time
}

func (s *PostgresStore) CreateSession(ctx context.Context, token string, userID int, expiresAt time.Time) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO sessions (token, user_id, expires_at) VALUES ($1,$2,$3)
	`, token, userID, expiresAt)
	return err
}

func (s *PostgresStore) GetSession(ctx context.Context, token string) (*SessionRow, error) {
	var sess SessionRow
	err := s.pool.QueryRow(ctx, `
		SELECT token, user_id, expires_at FROM sessions WHERE token=$1
	`, token).Scan(&sess.Token, &sess.UserID, &sess.ExpiresAt)
	if err != nil {
		return nil, fmt.Errorf("get session: %w", err)
	}
	return &sess, nil
}

func (s *PostgresStore) DeleteSession(ctx context.Context, token string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM sessions WHERE token=$1`, token)
	return err
}

func (s *PostgresStore) CleanExpiredSessions(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM sessions WHERE expires_at < NOW()`)
	return err
}

// Hot reload via PostgreSQL LISTEN/NOTIFY

// NotifyPolicyChange sends a pg_notify so gRPC server instances reload policies.
func (s *PostgresStore) NotifyPolicyChange(ctx context.Context) {
	if _, err := s.pool.Exec(ctx, "SELECT pg_notify('policy_changed', '')"); err != nil {
		log.Printf("policy notify: %v", err)
	}
}

// ListenPolicyChanges blocks, calling onReload on every 'policy_changed' notification.
// Reconnects automatically on connection loss. Stops when ctx is cancelled.
func (s *PostgresStore) ListenPolicyChanges(ctx context.Context, onReload func()) {
	for {
		if err := s.listenOnce(ctx, onReload); err != nil {
			if ctx.Err() != nil {
				return
			}
			log.Printf("policy watcher: %v — reconnecting in 5s", err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(5 * time.Second):
			}
		}
	}
}

func (s *PostgresStore) listenOnce(ctx context.Context, onReload func()) error {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire conn: %w", err)
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, "LISTEN policy_changed"); err != nil {
		return fmt.Errorf("LISTEN: %w", err)
	}
	log.Println("policy watcher: ready")

	for {
		if _, err := conn.Conn().WaitForNotification(ctx); err != nil {
			return fmt.Errorf("wait: %w", err)
		}
		log.Println("policy watcher: reload triggered")
		onReload()
	}
}
