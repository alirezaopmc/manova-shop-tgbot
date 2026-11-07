package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/alirezaopmc/manova-shop-tgbot/internal/domain/fee"
	"github.com/alirezaopmc/manova-shop-tgbot/internal/domain/user"
)

type userRepo struct {
	s *Store
}

func scanUser(row pgx.Row) (*user.User, error) {
	var (
		u          user.User
		roleStr    string
		segmentStr string
		fpsStr     string
	)
	err := row.Scan(
		&u.TelegramUserID,
		&u.Username,
		&u.FirstName,
		&u.LastName,
		&roleStr,
		&u.Locale,
		&segmentStr,
		&u.InvitedBy,
		&u.InviteCode,
		&fpsStr,
		&u.PhoneVerifiedAt,
		&u.PhoneE164,
		&u.PhoneHash,
		&u.Blocked,
		&u.CreatedAt,
		&u.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	u.Role = user.Role(roleStr)
	u.Segment = fee.Segment(segmentStr)
	u.FirstPurchaseState = fee.FirstPurchaseState(fpsStr)
	u.CreatedAt = u.CreatedAt.UTC()
	u.UpdatedAt = u.UpdatedAt.UTC()
	if u.PhoneVerifiedAt != nil {
		utc := u.PhoneVerifiedAt.UTC()
		u.PhoneVerifiedAt = &utc
	}
	return &u, nil
}

// GetByID retrieves a user record by their unique Telegram user ID.
// Returns ErrNotFound (aliased to memory.ErrNotFound) if the user does not exist.
func (r *userRepo) GetByID(ctx context.Context, userID int64) (*user.User, error) {
	if r.s == nil || r.s.pool == nil {
		return nil, errors.New("postgres: nil pool")
	}

	query := `
		SELECT telegram_user_id, username, first_name, last_name, role, locale, segment,
		       invited_by, invite_code, first_purchase_state, phone_verified_at,
		       phone_e164, phone_hash, blocked, created_at, updated_at
		FROM shop.users
		WHERE telegram_user_id = $1
	`
	u, err := scanUser(r.s.pool.QueryRow(ctx, query, userID))
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("postgres: get user by id %d: %w", userID, err)
	}
	return u, nil
}

// FindByPhoneHash retrieves a user record by HMAC-SHA256 phone hash.
// Returns ErrNotFound if no user is associated with this phone hash.
func (r *userRepo) FindByPhoneHash(ctx context.Context, phoneHash string) (*user.User, error) {
	if r.s == nil || r.s.pool == nil {
		return nil, errors.New("postgres: nil pool")
	}

	query := `
		SELECT telegram_user_id, username, first_name, last_name, role, locale, segment,
		       invited_by, invite_code, first_purchase_state, phone_verified_at,
		       phone_e164, phone_hash, blocked, created_at, updated_at
		FROM shop.users
		WHERE phone_hash = $1
	`
	u, err := scanUser(r.s.pool.QueryRow(ctx, query, phoneHash))
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("postgres: find user by phone hash: %w", err)
	}
	return u, nil
}

// Save persists the user using UPSERT (ON CONFLICT (telegram_user_id) DO UPDATE).
// Correctly handles phone unlinking/clearing by allowing NULL for phone fields.
func (r *userRepo) Save(ctx context.Context, u *user.User) error {
	if r.s == nil || r.s.pool == nil {
		return errors.New("postgres: nil pool")
	}
	if u == nil {
		return errors.New("postgres: nil user")
	}

	now := time.Now().UTC()
	createdAt := u.CreatedAt
	if createdAt.IsZero() {
		createdAt = now
		u.CreatedAt = createdAt
	}
	updatedAt := u.UpdatedAt
	if updatedAt.IsZero() {
		updatedAt = now
		u.UpdatedAt = updatedAt
	}

	role := string(u.Role)
	if role == "" {
		role = string(user.RoleUser)
	}
	locale := u.Locale
	if locale == "" {
		locale = "fa"
	}
	segment := string(u.Segment)
	if segment == "" {
		segment = string(fee.SegmentNew)
	}
	fps := string(u.FirstPurchaseState)
	if fps == "" {
		fps = string(fee.FirstPurchaseEligible)
	}

	var phoneVerifiedAt *time.Time
	if u.PhoneVerifiedAt != nil {
		utc := u.PhoneVerifiedAt.UTC()
		phoneVerifiedAt = &utc
	}

	query := `
		INSERT INTO shop.users (
			telegram_user_id, username, first_name, last_name, role, locale, segment,
			invited_by, invite_code, first_purchase_state, phone_verified_at,
			phone_e164, phone_hash, blocked, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16
		)
		ON CONFLICT (telegram_user_id) DO UPDATE SET
			username = EXCLUDED.username,
			first_name = EXCLUDED.first_name,
			last_name = EXCLUDED.last_name,
			role = EXCLUDED.role,
			locale = EXCLUDED.locale,
			segment = EXCLUDED.segment,
			invited_by = EXCLUDED.invited_by,
			invite_code = EXCLUDED.invite_code,
			first_purchase_state = EXCLUDED.first_purchase_state,
			phone_verified_at = EXCLUDED.phone_verified_at,
			phone_e164 = EXCLUDED.phone_e164,
			phone_hash = EXCLUDED.phone_hash,
			blocked = EXCLUDED.blocked,
			updated_at = EXCLUDED.updated_at
	`
	_, err := r.s.pool.Exec(ctx, query,
		u.TelegramUserID,
		u.Username,
		u.FirstName,
		u.LastName,
		role,
		locale,
		segment,
		u.InvitedBy,
		u.InviteCode,
		fps,
		phoneVerifiedAt,
		u.PhoneE164,
		u.PhoneHash,
		u.Blocked,
		createdAt.UTC(),
		updatedAt.UTC(),
	)
	if err != nil {
		return fmt.Errorf("postgres: save user %d: %w", u.TelegramUserID, err)
	}
	return nil
}

// Count returns total number of registered users.
func (r *userRepo) Count(ctx context.Context) (int64, error) {
	if r.s == nil || r.s.pool == nil {
		return 0, errors.New("postgres: nil pool")
	}

	var count int64
	err := r.s.pool.QueryRow(ctx, "SELECT COUNT(*) FROM shop.users").Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("postgres: count users: %w", err)
	}
	return count, nil
}

// ListUsers returns a page of users ordered by telegram_user_id ascending.
func (r *userRepo) ListUsers(ctx context.Context, limit int) ([]user.User, error) {
	if r.s == nil || r.s.pool == nil {
		return nil, errors.New("postgres: nil pool")
	}

	query := `
		SELECT telegram_user_id, username, first_name, last_name, role, locale, segment,
		       invited_by, invite_code, first_purchase_state, phone_verified_at,
		       phone_e164, phone_hash, blocked, created_at, updated_at
		FROM shop.users
		ORDER BY telegram_user_id ASC
	`
	var (
		rows pgx.Rows
		err  error
	)
	if limit > 0 {
		query += " LIMIT $1"
		rows, err = r.s.pool.Query(ctx, query, limit)
	} else {
		rows, err = r.s.pool.Query(ctx, query)
	}
	if err != nil {
		return nil, fmt.Errorf("postgres: list users: %w", err)
	}
	defer rows.Close()

	out := make([]user.User, 0)
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, fmt.Errorf("postgres: scan user in list: %w", err)
		}
		out = append(out, *u)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: list users rows error: %w", err)
	}
	return out, nil
}
