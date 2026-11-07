package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/alirezaopmc/manova-shop-tgbot/internal/domain/invite"
)

type inviteRepo struct {
	s *Store
}

func scanInvite(row pgx.Row) (*invite.Invite, error) {
	var inv invite.Invite
	err := row.Scan(
		&inv.Code,
		&inv.CreatedBy,
		&inv.MaxUses,
		&inv.UsesCount,
		&inv.ExpiresAt,
		&inv.IsActive,
		&inv.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, invite.ErrInviteNotFound
	}
	if err != nil {
		return nil, err
	}
	inv.CreatedAt = inv.CreatedAt.UTC()
	if inv.ExpiresAt != nil {
		utc := inv.ExpiresAt.UTC()
		inv.ExpiresAt = &utc
	}
	return &inv, nil
}

// Create inserts a new onboarding invite token.
func (r *inviteRepo) Create(ctx context.Context, inv *invite.Invite) error {
	if r.s == nil || r.s.pool == nil {
		return errors.New("postgres: nil pool")
	}
	if inv == nil {
		return errors.New("postgres: nil invite")
	}

	now := time.Now().UTC()
	createdAt := inv.CreatedAt
	if createdAt.IsZero() {
		createdAt = now
		inv.CreatedAt = createdAt
	}

	var expiresAt *time.Time
	if inv.ExpiresAt != nil {
		utc := inv.ExpiresAt.UTC()
		expiresAt = &utc
	}

	query := `
		INSERT INTO shop.invites (
			code, created_by, max_uses, uses_count, expires_at, is_active, created_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7
		)
	`
	_, err := r.s.pool.Exec(ctx, query,
		inv.Code,
		inv.CreatedBy,
		inv.MaxUses,
		inv.UsesCount,
		expiresAt,
		inv.IsActive,
		createdAt.UTC(),
	)
	if err != nil {
		return fmt.Errorf("postgres: create invite %s: %w", inv.Code, err)
	}
	return nil
}

// GetByCode retrieves an invite by its exact token code.
// Returns invite.ErrInviteNotFound if missing.
func (r *inviteRepo) GetByCode(ctx context.Context, code string) (*invite.Invite, error) {
	if r.s == nil || r.s.pool == nil {
		return nil, errors.New("postgres: nil pool")
	}

	query := `
		SELECT code, created_by, max_uses, uses_count, expires_at, is_active, created_at
		FROM shop.invites
		WHERE code = $1
	`
	inv, err := scanInvite(r.s.pool.QueryRow(ctx, query, code))
	if err != nil {
		if errors.Is(err, invite.ErrInviteNotFound) {
			return nil, invite.ErrInviteNotFound
		}
		return nil, fmt.Errorf("postgres: get invite by code %s: %w", code, err)
	}
	return inv, nil
}

// Claim atomically claims an invite within a pessimistic-locking transaction.
// Enforces anti-duplicate claiming per user via shop.invite_claims.
func (r *inviteRepo) Claim(ctx context.Context, code string, userID int64, now time.Time) (*invite.Invite, error) {
	if r.s == nil || r.s.pool == nil {
		return nil, errors.New("postgres: nil pool")
	}

	tx, err := r.s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("postgres: begin claim tx: %w", err)
	}
	defer tx.Rollback(ctx)

	// Step 1: Pessimistic lock on the invite row
	selectQuery := `
		SELECT code, created_by, max_uses, uses_count, expires_at, is_active, created_at
		FROM shop.invites
		WHERE code = $1
		FOR UPDATE
	`
	inv, err := scanInvite(tx.QueryRow(ctx, selectQuery, code))
	if err != nil {
		if errors.Is(err, invite.ErrInviteNotFound) {
			return nil, invite.ErrInviteNotFound
		}
		return nil, fmt.Errorf("postgres: lock invite %s: %w", code, err)
	}

	// Step 2: Check if this user has already claimed this invite code
	var exists int
	claimCheckQuery := `
		SELECT 1 FROM shop.invite_claims
		WHERE invite_code = $1 AND user_id = $2
	`
	err = tx.QueryRow(ctx, claimCheckQuery, code, userID).Scan(&exists)
	if err == nil {
		return nil, invite.ErrInviteAlreadyUsed
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("postgres: check invite claim: %w", err)
	}

	// Step 3: Validate domain business rules and update in-memory entity
	if err := inv.Claim(userID, now); err != nil {
		return nil, err
	}

	// Step 4: Persist updated uses_count and is_active
	updateQuery := `
		UPDATE shop.invites
		SET uses_count = $1, is_active = $2
		WHERE code = $3
	`
	if _, err := tx.Exec(ctx, updateQuery, inv.UsesCount, inv.IsActive, inv.Code); err != nil {
		return nil, fmt.Errorf("postgres: update invite %s: %w", inv.Code, err)
	}

	// Step 5: Record claim in shop.invite_claims
	insertClaimQuery := `
		INSERT INTO shop.invite_claims (invite_code, user_id, claimed_at)
		VALUES ($1, $2, $3)
	`
	if _, err := tx.Exec(ctx, insertClaimQuery, inv.Code, userID, now.UTC()); err != nil {
		return nil, fmt.Errorf("postgres: insert invite claim: %w", err)
	}

	// Step 6: Commit transaction
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("postgres: commit claim tx: %w", err)
	}

	return inv, nil
}

// ListByCreator returns all invites created by the specified admin, ordered latest first.
func (r *inviteRepo) ListByCreator(ctx context.Context, adminID int64) ([]invite.Invite, error) {
	if r.s == nil || r.s.pool == nil {
		return nil, errors.New("postgres: nil pool")
	}

	query := `
		SELECT code, created_by, max_uses, uses_count, expires_at, is_active, created_at
		FROM shop.invites
		WHERE created_by = $1
		ORDER BY created_at DESC
	`
	rows, err := r.s.pool.Query(ctx, query, adminID)
	if err != nil {
		return nil, fmt.Errorf("postgres: list invites by creator %d: %w", adminID, err)
	}
	defer rows.Close()

	out := make([]invite.Invite, 0)
	for rows.Next() {
		inv, err := scanInvite(rows)
		if err != nil {
			return nil, fmt.Errorf("postgres: scan invite in list: %w", err)
		}
		out = append(out, *inv)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: list invites rows error: %w", err)
	}
	return out, nil
}
