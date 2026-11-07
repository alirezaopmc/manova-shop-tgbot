package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/alirezaopmc/manova-shop-tgbot/internal/domain/receipt"
)

type receiptRepo struct {
	s *Store
}

func scanReceipt(row pgx.Row) (*receipt.Receipt, error) {
	var (
		rcp             receipt.Receipt
		purposeStr      string
		statusStr       string
		rejectReasonStr *string
	)
	err := row.Scan(
		&rcp.ID,
		&rcp.UserID,
		&purposeStr,
		&rcp.OrderID,
		&rcp.QuoteID,
		&rcp.AmountIRR,
		&rcp.AmountIRT,
		&rcp.TrackingCodeNorm,
		&rcp.ImageSHA256,
		&rcp.ImageRef,
		&statusStr,
		&rcp.DecidedByAdminID,
		&rcp.DecidedAt,
		&rejectReasonStr,
		&rcp.Note,
		&rcp.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	rcp.Purpose = receipt.Purpose(purposeStr)
	rcp.Status = receipt.Status(statusStr)
	if rejectReasonStr != nil {
		rr := receipt.RejectReason(*rejectReasonStr)
		rcp.RejectReason = &rr
	}
	rcp.CreatedAt = rcp.CreatedAt.UTC()
	if rcp.DecidedAt != nil {
		t := rcp.DecidedAt.UTC()
		rcp.DecidedAt = &t
	}
	return &rcp, nil
}

// GetByID retrieves a payment receipt by its unique identifier.
// Returns ErrNotFound if the receipt does not exist.
func (r *receiptRepo) GetByID(ctx context.Context, receiptID string) (*receipt.Receipt, error) {
	if r.s == nil || r.s.pool == nil {
		return nil, errors.New("postgres: nil pool")
	}
	trimmed := strings.TrimSpace(receiptID)
	if trimmed == "" {
		return nil, ErrNotFound
	}

	query := `
		SELECT id, user_id, purpose, order_id, quote_id,
		       amount_irr, amount_irt, tracking_code_norm, image_sha256, image_ref,
		       status, decided_by_admin_id, decided_at, reject_reason, note, created_at
		FROM shop.receipts
		WHERE id = $1
	`
	rcp, err := scanReceipt(r.s.pool.QueryRow(ctx, query, trimmed))
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("postgres: get receipt by id %s: %w", receiptID, err)
	}
	return rcp, nil
}

// FindByImageSHA256 searches for an existing receipt with the specified SHA256 image digest.
// Returns ErrNotFound if no receipt has this image hash.
func (r *receiptRepo) FindByImageSHA256(ctx context.Context, hash string) (*receipt.Receipt, error) {
	if r.s == nil || r.s.pool == nil {
		return nil, errors.New("postgres: nil pool")
	}
	trimmed := strings.TrimSpace(hash)
	if trimmed == "" {
		return nil, ErrNotFound
	}

	query := `
		SELECT id, user_id, purpose, order_id, quote_id,
		       amount_irr, amount_irt, tracking_code_norm, image_sha256, image_ref,
		       status, decided_by_admin_id, decided_at, reject_reason, note, created_at
		FROM shop.receipts
		WHERE image_sha256 = $1
	`
	rcp, err := scanReceipt(r.s.pool.QueryRow(ctx, query, trimmed))
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("postgres: find receipt by image hash: %w", err)
	}
	return rcp, nil
}

// FindByTrackingCode searches for a receipt matching the supplied tracking code.
// The tracking code is matched against normalized tracking code representations.
// Returns ErrNotFound if no match is found.
func (r *receiptRepo) FindByTrackingCode(ctx context.Context, trackingCode string) (*receipt.Receipt, error) {
	if r.s == nil || r.s.pool == nil {
		return nil, errors.New("postgres: nil pool")
	}
	trimmed := strings.TrimSpace(trackingCode)
	if trimmed == "" {
		return nil, ErrNotFound
	}
	norm := receipt.NormalizeTrackingCode(trimmed)
	if norm == "" {
		norm = trimmed
	}

	query := `
		SELECT id, user_id, purpose, order_id, quote_id,
		       amount_irr, amount_irt, tracking_code_norm, image_sha256, image_ref,
		       status, decided_by_admin_id, decided_at, reject_reason, note, created_at
		FROM shop.receipts
		WHERE tracking_code_norm = $1 OR tracking_code_norm = $2
		LIMIT 1
	`
	rcp, err := scanReceipt(r.s.pool.QueryRow(ctx, query, norm, trimmed))
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("postgres: find receipt by tracking code %q: %w", trackingCode, err)
	}
	return rcp, nil
}

// Save inserts or updates a receipt record in shop.receipts.
// Unique constraints on image_sha256 and tracking_code_norm ensure anti-replay protection.
func (r *receiptRepo) Save(ctx context.Context, rcp *receipt.Receipt) error {
	if r.s == nil || r.s.pool == nil {
		return errors.New("postgres: nil pool")
	}
	if rcp == nil {
		return errors.New("postgres: nil receipt")
	}
	if strings.TrimSpace(rcp.ID) == "" {
		return errors.New("postgres: receipt ID cannot be empty")
	}
	if strings.TrimSpace(rcp.ImageSHA256) == "" {
		return errors.New("postgres: receipt image hash cannot be empty")
	}
	if strings.TrimSpace(rcp.TrackingCodeNorm) == "" {
		return errors.New("postgres: receipt tracking code cannot be empty")
	}

	norm := receipt.NormalizeTrackingCode(rcp.TrackingCodeNorm)
	if norm != "" {
		rcp.TrackingCodeNorm = norm
	}

	now := time.Now().UTC()
	if rcp.CreatedAt.IsZero() {
		rcp.CreatedAt = now
	} else {
		rcp.CreatedAt = rcp.CreatedAt.UTC()
	}

	var decidedAt *time.Time
	if rcp.DecidedAt != nil {
		t := rcp.DecidedAt.UTC()
		decidedAt = &t
	}

	var rejectReason *string
	if rcp.RejectReason != nil {
		str := string(*rcp.RejectReason)
		rejectReason = &str
	}

	query := `
		INSERT INTO shop.receipts (
			id, user_id, purpose, order_id, quote_id,
			amount_irr, amount_irt, tracking_code_norm, image_sha256, image_ref,
			status, decided_by_admin_id, decided_at, reject_reason, note, created_at
		) VALUES (
			$1, $2, $3, $4, $5,
			$6, $7, $8, $9, $10,
			$11, $12, $13, $14, $15, $16
		)
		ON CONFLICT (id) DO UPDATE SET
			user_id = EXCLUDED.user_id,
			purpose = EXCLUDED.purpose,
			order_id = EXCLUDED.order_id,
			quote_id = EXCLUDED.quote_id,
			amount_irr = EXCLUDED.amount_irr,
			amount_irt = EXCLUDED.amount_irt,
			tracking_code_norm = EXCLUDED.tracking_code_norm,
			image_sha256 = EXCLUDED.image_sha256,
			image_ref = EXCLUDED.image_ref,
			status = EXCLUDED.status,
			decided_by_admin_id = EXCLUDED.decided_by_admin_id,
			decided_at = EXCLUDED.decided_at,
			reject_reason = EXCLUDED.reject_reason,
			note = EXCLUDED.note
	`
	_, err := r.s.pool.Exec(
		ctx,
		query,
		rcp.ID,
		rcp.UserID,
		string(rcp.Purpose),
		rcp.OrderID,
		rcp.QuoteID,
		int64(rcp.AmountIRR),
		int64(rcp.AmountIRT),
		rcp.TrackingCodeNorm,
		rcp.ImageSHA256,
		rcp.ImageRef,
		string(rcp.Status),
		rcp.DecidedByAdminID,
		decidedAt,
		rejectReason,
		rcp.Note,
		rcp.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("postgres: save receipt %s: %w", rcp.ID, err)
	}
	return nil
}
