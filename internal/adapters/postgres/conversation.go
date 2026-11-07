package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// ConversationStore provides persistent storage for conversational wizard states
// (e.g. buyDrafts and pendingTopups) across bot restarts.
type ConversationStore struct {
	s *Store
}

type conversationEnvelope struct {
	Step      string          `json:"step"`
	Data      json.RawMessage `json:"data"`
	ExpiresAt *time.Time      `json:"expires_at,omitempty"`
}

// SaveConversationState persists an in-flight conversation state for a user and flow.
func (c *ConversationStore) SaveConversationState(ctx context.Context, userID int64, flow string, step string, data any, expiresAt time.Time) error {
	if c.s == nil || c.s.pool == nil {
		return errors.New("postgres: nil pool")
	}
	if userID <= 0 {
		return errors.New("postgres: invalid userID for conversation state")
	}
	if flow == "" {
		return errors.New("postgres: empty flow for conversation state")
	}

	var dataRaw json.RawMessage
	if data != nil {
		b, err := json.Marshal(data)
		if err != nil {
			return fmt.Errorf("postgres: marshal conversation state data: %w", err)
		}
		dataRaw = b
	} else {
		dataRaw = []byte("null")
	}

	env := conversationEnvelope{
		Step: step,
		Data: dataRaw,
	}
	if !expiresAt.IsZero() {
		exp := expiresAt.UTC()
		env.ExpiresAt = &exp
	}

	envBytes, err := json.Marshal(env)
	if err != nil {
		return fmt.Errorf("postgres: marshal conversation envelope: %w", err)
	}

	query := `
		INSERT INTO shop.conversation_states (user_id, flow, state, updated_at)
		VALUES ($1, $2, $3, NOW())
		ON CONFLICT (user_id, flow) DO UPDATE SET
			state = EXCLUDED.state,
			updated_at = NOW()
	`
	if _, err := c.s.pool.Exec(ctx, query, userID, flow, envBytes); err != nil {
		return fmt.Errorf("postgres: save conversation state (user=%d, flow=%s): %w", userID, flow, err)
	}
	return nil
}

// GetConversationState retrieves and decodes a saved conversation state.
// If the state is expired or not found, it returns found = false without error.
func (c *ConversationStore) GetConversationState(ctx context.Context, userID int64, flow string, dest any) (step string, found bool, err error) {
	if c.s == nil || c.s.pool == nil {
		return "", false, errors.New("postgres: nil pool")
	}
	if userID <= 0 || flow == "" {
		return "", false, nil
	}

	query := `
		SELECT state
		FROM shop.conversation_states
		WHERE user_id = $1 AND flow = $2
	`
	var stateBytes []byte
	err = c.s.pool.QueryRow(ctx, query, userID, flow).Scan(&stateBytes)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("postgres: get conversation state (user=%d, flow=%s): %w", userID, flow, err)
	}

	var env conversationEnvelope
	if err := json.Unmarshal(stateBytes, &env); err != nil {
		return "", false, fmt.Errorf("postgres: unmarshal conversation envelope: %w", err)
	}

	if env.ExpiresAt != nil && !env.ExpiresAt.IsZero() && time.Now().UTC().After(*env.ExpiresAt) {
		// State has expired; delete it lazily and return not found.
		_ = c.DeleteConversationState(ctx, userID, flow)
		return "", false, nil
	}

	if dest != nil && len(env.Data) > 0 && string(env.Data) != "null" {
		if err := json.Unmarshal(env.Data, dest); err != nil {
			return "", false, fmt.Errorf("postgres: unmarshal conversation state data into dest: %w", err)
		}
	}

	return env.Step, true, nil
}

// DeleteConversationState removes any active conversation state for a user and flow.
func (c *ConversationStore) DeleteConversationState(ctx context.Context, userID int64, flow string) error {
	if c.s == nil || c.s.pool == nil {
		return errors.New("postgres: nil pool")
	}
	if userID <= 0 || flow == "" {
		return nil
	}

	query := `
		DELETE FROM shop.conversation_states
		WHERE user_id = $1 AND flow = $2
	`
	if _, err := c.s.pool.Exec(ctx, query, userID, flow); err != nil {
		return fmt.Errorf("postgres: delete conversation state (user=%d, flow=%s): %w", userID, flow, err)
	}
	return nil
}

// Save is a convenience alias for SaveConversationState.
func (c *ConversationStore) Save(ctx context.Context, userID int64, flow string, step string, data any, expiresAt time.Time) error {
	return c.SaveConversationState(ctx, userID, flow, step, data, expiresAt)
}

// Get is a convenience alias for GetConversationState.
func (c *ConversationStore) Get(ctx context.Context, userID int64, flow string, dest any) (step string, found bool, err error) {
	return c.GetConversationState(ctx, userID, flow, dest)
}

// Delete is a convenience alias for DeleteConversationState.
func (c *ConversationStore) Delete(ctx context.Context, userID int64, flow string) error {
	return c.DeleteConversationState(ctx, userID, flow)
}
