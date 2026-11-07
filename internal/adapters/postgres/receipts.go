package postgres

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/alirezaopmc/manova-shop-tgbot/internal/domain/pricing"
	"github.com/alirezaopmc/manova-shop-tgbot/internal/domain/receipt"
	"github.com/alirezaopmc/manova-shop-tgbot/internal/domain/wallet"
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

// ApproveReceipt atomically approves the receipt, credits the user's wallet,
// and appends an immutable topup ledger entry within a single transaction.
func (r *receiptRepo) ApproveReceipt(
	ctx context.Context,
	receiptID string,
	adminID int64,
	amountIRT pricing.IRT,
	now time.Time,
) (*receipt.Receipt, *wallet.Wallet, error) {
	if r.s == nil || r.s.pool == nil {
		return nil, nil, errors.New("postgres: nil pool")
	}
	trimmed := strings.TrimSpace(receiptID)
	if trimmed == "" {
		return nil, nil, ErrNotFound
	}
	if amountIRT <= 0 {
		return nil, nil, errors.New("postgres: amount must be positive")
	}
	if now.IsZero() {
		now = time.Now().UTC()
	} else {
		now = now.UTC()
	}

	tx, err := r.s.pool.Begin(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("postgres: begin approve receipt tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// 1. Lock and fetch receipt
	queryReceipt := `
		SELECT id, user_id, purpose, order_id, quote_id,
		       amount_irr, amount_irt, tracking_code_norm, image_sha256, image_ref,
		       status, decided_by_admin_id, decided_at, reject_reason, note, created_at
		FROM shop.receipts
		WHERE id = $1
		FOR UPDATE
	`
	rcp, err := scanReceipt(tx.QueryRow(ctx, queryReceipt, trimmed))
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, nil, ErrNotFound
		}
		return nil, nil, fmt.Errorf("postgres: lock receipt for approval: %w", err)
	}

	if err := rcp.Approve(adminID, now); err != nil {
		return nil, nil, err
	}
	rcp.AmountIRT = amountIRT

	updateReceiptQuery := `
		UPDATE shop.receipts
		SET status = $1,
		    decided_by_admin_id = $2,
		    decided_at = $3,
		    amount_irt = $4
		WHERE id = $5
	`
	if _, err := tx.Exec(
		ctx,
		updateReceiptQuery,
		string(rcp.Status),
		rcp.DecidedByAdminID,
		rcp.DecidedAt,
		int64(rcp.AmountIRT),
		rcp.ID,
	); err != nil {
		return nil, nil, fmt.Errorf("postgres: update receipt on approval: %w", err)
	}

	// 2. Lock and credit wallet
	queryWallet := `
		SELECT user_id, id, balance_irt, balance_usd_micro, floor_override_irt, version
		FROM shop.wallets
		WHERE user_id = $1
		FOR UPDATE
	`
	var (
		uid             int64
		wid             int64
		balanceIRT      int64
		balanceUSDMicro int64
		floorOverride   *int64
		version         int64
	)
	err = tx.QueryRow(ctx, queryWallet, rcp.UserID).Scan(
		&uid, &wid, &balanceIRT, &balanceUSDMicro, &floorOverride, &version,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		insertWallet := `
			INSERT INTO shop.wallets (
				user_id, id, balance_irt, balance_usd_micro, version, updated_at
			) VALUES (
				$1, $1, 0, 0, 1, NOW()
			)
			RETURNING user_id, id, balance_irt, balance_usd_micro, floor_override_irt, version
		`
		if err := tx.QueryRow(ctx, insertWallet, rcp.UserID).Scan(
			&uid, &wid, &balanceIRT, &balanceUSDMicro, &floorOverride, &version,
		); err != nil {
			return nil, nil, fmt.Errorf("postgres: init wallet in receipt approval tx: %w", err)
		}
	} else if err != nil {
		return nil, nil, fmt.Errorf("postgres: lock wallet for receipt approval: %w", err)
	}

	if wid == 0 {
		wid = uid
	}

	w := &wallet.Wallet{
		ID:              wid,
		UserID:          uid,
		BalanceIRT:      pricing.IRT(balanceIRT) + amountIRT,
		BalanceUSDMicro: pricing.MicroUSD(balanceUSDMicro),
		Version:         version,
	}
	if floorOverride != nil {
		f := pricing.IRT(*floorOverride)
		w.FloorOverrideIRT = &f
	}

	var floorVal *int64
	if w.FloorOverrideIRT != nil {
		val := int64(*w.FloorOverrideIRT)
		floorVal = &val
	}

	updateWalletQuery := `
		UPDATE shop.wallets
		SET balance_irt = $1,
		    balance_usd_micro = $2,
		    floor_override_irt = $3,
		    version = version + 1,
		    updated_at = NOW()
		WHERE user_id = $4
		RETURNING version
	`
	var newVersion int64
	if err := tx.QueryRow(
		ctx,
		updateWalletQuery,
		int64(w.BalanceIRT),
		int64(w.BalanceUSDMicro),
		floorVal,
		rcp.UserID,
	).Scan(&newVersion); err != nil {
		return nil, nil, fmt.Errorf("postgres: update balance in receipt approval tx: %w", err)
	}
	w.Version = newVersion

	// 3. Record immutable ledger entry
	salt := make([]byte, 4)
	_, _ = rand.Read(salt)
	hexSalt := hex.EncodeToString(salt)
	ledgID := fmt.Sprintf("ledg-rcpt-%d-%s", now.UnixNano(), hexSalt)

	ledgerQuery := `
		INSERT INTO shop.wallet_ledger (
			id, wallet_id, currency, amount, kind, balance_after, ref_type, ref_id, created_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9
		)
	`
	if _, err := tx.Exec(
		ctx,
		ledgerQuery,
		ledgID,
		w.ID,
		string(wallet.CurrencyIRT),
		int64(amountIRT),
		string(wallet.KindTopupReceipt),
		int64(w.BalanceIRT),
		"receipt",
		rcp.ID,
		now,
	); err != nil {
		return nil, nil, fmt.Errorf("postgres: record receipt approval ledger: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, nil, fmt.Errorf("postgres: commit receipt approval tx: %w", err)
	}

	return rcp, w, nil
}
