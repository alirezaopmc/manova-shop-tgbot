package postgres

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/alirezaopmc/manova-shop-tgbot/internal/domain/order"
	"github.com/alirezaopmc/manova-shop-tgbot/internal/domain/pricing"
)

type orderRepo struct {
	s *Store
}

func scanOrder(row pgx.Row) (*order.Order, error) {
	var (
		o                    order.Order
		statusStr            string
		checkoutModeStr      string
		lockedQuoteBytes     []byte
		checkoutCreditIRTRaw *int64
	)
	err := row.Scan(
		&o.ID,
		&o.UserID,
		&statusStr,
		&checkoutModeStr,
		&o.HoldReason,
		&o.QuoteID,
		&lockedQuoteBytes,
		&checkoutCreditIRTRaw,
		&o.CardAccountID,
		&o.PolicyVersion,
		&o.PolicyAcceptedAt,
		&o.ExpiresAt,
		&o.Version,
		&o.CreatedAt,
		&o.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	o.Status = order.State(statusStr)
	o.CheckoutMode = order.CheckoutMode(checkoutModeStr)
	if checkoutCreditIRTRaw != nil {
		cc := pricing.IRT(*checkoutCreditIRTRaw)
		o.CheckoutCreditIRT = &cc
	}
	if len(lockedQuoteBytes) > 0 && string(lockedQuoteBytes) != "{}" && string(lockedQuoteBytes) != "null" {
		if err := json.Unmarshal(lockedQuoteBytes, &o.LockedQuote); err != nil {
			return nil, fmt.Errorf("postgres: unmarshal locked quote: %w", err)
		}
		o.LockedQuote.CreatedAt = o.LockedQuote.CreatedAt.UTC()
	}
	o.PolicyAcceptedAt = o.PolicyAcceptedAt.UTC()
	o.ExpiresAt = o.ExpiresAt.UTC()
	o.CreatedAt = o.CreatedAt.UTC()
	o.UpdatedAt = o.UpdatedAt.UTC()
	return &o, nil
}

// GetByID retrieves an order by its unique ID.
// Returns ErrNotFound if the order does not exist.
func (r *orderRepo) GetByID(ctx context.Context, orderID string) (*order.Order, error) {
	if r.s == nil || r.s.pool == nil {
		return nil, errors.New("postgres: nil pool")
	}
	trimmed := strings.TrimSpace(orderID)
	if trimmed == "" {
		return nil, ErrNotFound
	}

	query := `
		SELECT id, user_id, status, checkout_mode, hold_reason,
		       quote_id, locked_quote, checkout_credit_irt, card_account_id,
		       policy_version, policy_accepted_at, expires_at,
		       version, created_at, updated_at
		FROM shop.orders
		WHERE id = $1
	`
	o, err := scanOrder(r.s.pool.QueryRow(ctx, query, trimmed))
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("postgres: get order by id %s: %w", orderID, err)
	}
	return o, nil
}

// Save inserts or updates an order in shop.orders, and if an event is provided,
// inserts the transition audit record into shop.order_events within the same transaction.
func (r *orderRepo) Save(ctx context.Context, o *order.Order, event *order.OrderEvent) error {
	if r.s == nil || r.s.pool == nil {
		return errors.New("postgres: nil pool")
	}
	if o == nil {
		return errors.New("postgres: nil order")
	}
	if strings.TrimSpace(o.ID) == "" {
		return errors.New("postgres: order ID cannot be empty")
	}

	now := time.Now().UTC()
	if o.CreatedAt.IsZero() {
		o.CreatedAt = now
	} else {
		o.CreatedAt = o.CreatedAt.UTC()
	}
	if o.UpdatedAt.IsZero() {
		o.UpdatedAt = now
	} else {
		o.UpdatedAt = o.UpdatedAt.UTC()
	}
	if o.PolicyAcceptedAt.IsZero() {
		o.PolicyAcceptedAt = o.CreatedAt
	} else {
		o.PolicyAcceptedAt = o.PolicyAcceptedAt.UTC()
	}
	if o.ExpiresAt.IsZero() {
		o.ExpiresAt = now.Add(30 * time.Minute)
	} else {
		o.ExpiresAt = o.ExpiresAt.UTC()
	}
	if o.Version == 0 {
		o.Version = 1
	}

	lockedQuoteJSON, err := json.Marshal(o.LockedQuote)
	if err != nil {
		return fmt.Errorf("postgres: marshal locked quote: %w", err)
	}

	var checkoutCredit *int64
	if o.CheckoutCreditIRT != nil {
		val := int64(*o.CheckoutCreditIRT)
		checkoutCredit = &val
	}

	tx, err := r.s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("postgres: begin order save tx: %w", err)
	}
	defer tx.Rollback(ctx)

	orderUpsert := `
		INSERT INTO shop.orders (
			id, user_id, status, checkout_mode, hold_reason,
			quote_id, locked_quote, checkout_credit_irt, card_account_id,
			policy_version, policy_accepted_at, expires_at,
			version, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5,
			$6, $7, $8, $9,
			$10, $11, $12,
			$13, $14, $15
		)
		ON CONFLICT (id) DO UPDATE SET
			user_id = EXCLUDED.user_id,
			status = EXCLUDED.status,
			checkout_mode = EXCLUDED.checkout_mode,
			hold_reason = EXCLUDED.hold_reason,
			quote_id = EXCLUDED.quote_id,
			locked_quote = EXCLUDED.locked_quote,
			checkout_credit_irt = EXCLUDED.checkout_credit_irt,
			card_account_id = EXCLUDED.card_account_id,
			policy_version = EXCLUDED.policy_version,
			policy_accepted_at = EXCLUDED.policy_accepted_at,
			expires_at = EXCLUDED.expires_at,
			version = EXCLUDED.version,
			updated_at = EXCLUDED.updated_at
	`
	_, err = tx.Exec(
		ctx,
		orderUpsert,
		o.ID,
		o.UserID,
		string(o.Status),
		string(o.CheckoutMode),
		o.HoldReason,
		o.QuoteID,
		lockedQuoteJSON,
		checkoutCredit,
		o.CardAccountID,
		o.PolicyVersion,
		o.PolicyAcceptedAt,
		o.ExpiresAt,
		o.Version,
		o.CreatedAt,
		o.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("postgres: save order %s: %w", o.ID, err)
	}

	if event != nil {
		if event.ID == "" {
			var b [8]byte
			_, _ = rand.Read(b[:])
			event.ID = fmt.Sprintf("ordevt-%d-%x", time.Now().UnixNano(), b)
		}
		if event.OrderID == "" {
			event.OrderID = o.ID
		}
		if event.CreatedAt.IsZero() {
			event.CreatedAt = now
		} else {
			event.CreatedAt = event.CreatedAt.UTC()
		}

		eventInsert := `
			INSERT INTO shop.order_events (
				id, order_id, from_status, to_status, source, source_ref, created_at
			) VALUES (
				$1, $2, $3, $4, $5, $6, $7
			)
		`
		_, err = tx.Exec(
			ctx,
			eventInsert,
			event.ID,
			event.OrderID,
			string(event.FromStatus),
			string(event.ToStatus),
			event.Source,
			event.SourceRef,
			event.CreatedAt,
		)
		if err != nil {
			return fmt.Errorf("postgres: insert order event for order %s: %w", o.ID, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("postgres: commit order save tx: %w", err)
	}
	return nil
}

// GetEventsByOrderID retrieves the event audit log for a given order ID, sorted chronologically.
func (r *orderRepo) GetEventsByOrderID(ctx context.Context, orderID string) ([]order.OrderEvent, error) {
	if r.s == nil || r.s.pool == nil {
		return nil, errors.New("postgres: nil pool")
	}
	trimmed := strings.TrimSpace(orderID)
	if trimmed == "" {
		return nil, nil
	}

	query := `
		SELECT id, order_id, from_status, to_status, source, source_ref, created_at
		FROM shop.order_events
		WHERE order_id = $1
		ORDER BY created_at ASC, id ASC
	`
	rows, err := r.s.pool.Query(ctx, query, trimmed)
	if err != nil {
		return nil, fmt.Errorf("postgres: query order events: %w", err)
	}
	defer rows.Close()

	var events []order.OrderEvent
	for rows.Next() {
		var (
			e       order.OrderEvent
			fromStr string
			toStr   string
		)
		if err := rows.Scan(&e.ID, &e.OrderID, &fromStr, &toStr, &e.Source, &e.SourceRef, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("postgres: scan order event: %w", err)
		}
		e.FromStatus = order.State(fromStr)
		e.ToStatus = order.State(toStr)
		e.CreatedAt = e.CreatedAt.UTC()
		events = append(events, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: iterate order events: %w", err)
	}
	return events, nil
}
