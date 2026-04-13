package storage

import (
	"context"
	"encoding/json"
	"fmt"
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

func (s *PostgresStore) LoadPolicies(ctx context.Context) ([]policy.Policy, error) {
	const query = `
		SELECT policy_id, type, priority, selectors, steps,
		       COALESCE(conditional_steps, '[]'::jsonb)
		FROM policies
		WHERE enabled = TRUE
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

// CRUD

type PolicyRow struct {
	ID               int64           `json:"id"`
	PolicyID         string          `json:"policy_id"`
	Type             string          `json:"type"`
	Priority         int             `json:"priority"`
	Selectors        json.RawMessage `json:"selectors"`
	Steps            json.RawMessage `json:"steps"`
	ConditionalSteps json.RawMessage `json:"conditional_steps"`
	Enabled          bool            `json:"enabled"`
}

func (s *PostgresStore) CreatePolicy(ctx context.Context, row PolicyRow) error {
	condSteps := row.ConditionalSteps
	if condSteps == nil {
		condSteps = []byte("[]")
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO policies (policy_id, type, priority, selectors, steps, conditional_steps, enabled)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, row.PolicyID, row.Type, row.Priority, row.Selectors, row.Steps, condSteps, row.Enabled)
	return err
}

func (s *PostgresStore) UpdatePolicy(ctx context.Context, row PolicyRow) error {
	condSteps := row.ConditionalSteps
	if condSteps == nil {
		condSteps = []byte("[]")
	}
	tag, err := s.pool.Exec(ctx, `
		UPDATE policies SET type=$2, priority=$3, selectors=$4, steps=$5, conditional_steps=$6, enabled=$7
		WHERE policy_id = $1
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
	tag, err := s.pool.Exec(ctx, `DELETE FROM policies WHERE policy_id = $1`, policyID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("policy %s not found", policyID)
	}
	return nil
}

func (s *PostgresStore) DisablePolicy(ctx context.Context, policyID string) error {
	_, err := s.pool.Exec(ctx, `UPDATE policies SET enabled = FALSE WHERE policy_id = $1`, policyID)
	return err
}

func (s *PostgresStore) GetPolicy(ctx context.Context, policyID string) (*PolicyRow, error) {
	var row PolicyRow
	err := s.pool.QueryRow(ctx, `
		SELECT id, policy_id, type, priority, selectors, steps,
		       COALESCE(conditional_steps, '[]'::jsonb), enabled
		FROM policies WHERE policy_id = $1
	`, policyID).Scan(&row.ID, &row.PolicyID, &row.Type, &row.Priority,
		&row.Selectors, &row.Steps, &row.ConditionalSteps, &row.Enabled)
	if err != nil {
		return nil, fmt.Errorf("get policy %s: %w", policyID, err)
	}
	return &row, nil
}
