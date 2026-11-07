package postgres

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/alirezaopmc/manova-shop-tgbot/internal/domain/fee"
	"github.com/alirezaopmc/manova-shop-tgbot/internal/domain/pricing"
	"github.com/alirezaopmc/manova-shop-tgbot/internal/domain/wallet"
)

type walletRepo struct {
	s *Store
}

// GetByUserID retrieves the user's wallet. If no wallet exists for userID,
// a default zero-balance wallet (balance_irt = 0, balance_usd_micro = 0, version = 1)
// is atomically inserted and returned, matching memory.Store behavior.
func (r *walletRepo) GetByUserID(ctx context.Context, userID int64) (*wallet.Wallet, error) {
	if r.s == nil || r.s.pool == nil {
		return nil, errors.New("postgres: nil pool")
	}

	w, err := r.queryWallet(ctx, userID)
	if err == nil {
		return w, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("postgres: get wallet for user %d: %w", userID, err)
	}

	// Not found: insert default wallet row
	insertQuery := `
		INSERT INTO shop.wallets (
			user_id, id, balance_irt, balance_usd_micro, version, updated_at
		) VALUES (
			$1, $1, 0, 0, 1, NOW()
		)
		ON CONFLICT (user_id) DO NOTHING
	`
	if _, err := r.s.pool.Exec(ctx, insertQuery, userID); err != nil {
		return nil, fmt.Errorf("postgres: auto-create wallet for user %d: %w", userID, err)
	}

	w, err = r.queryWallet(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("postgres: query created wallet for user %d: %w", userID, err)
	}
	return w, nil
}

func (r *walletRepo) queryWallet(ctx context.Context, userID int64) (*wallet.Wallet, error) {
	query := `
		SELECT user_id, id, balance_irt, balance_usd_micro, floor_override_irt, version
		FROM shop.wallets
		WHERE user_id = $1
	`
	var (
		uid             int64
		wid             int64
		balanceIRT      int64
		balanceUSDMicro int64
		floorOverride   *int64
		version         int64
	)
	err := r.s.pool.QueryRow(ctx, query, userID).Scan(
		&uid, &wid, &balanceIRT, &balanceUSDMicro, &floorOverride, &version,
	)
	if err != nil {
		return nil, err
	}
	if wid == 0 {
		wid = uid
	}
	w := &wallet.Wallet{
		ID:              wid,
		UserID:          uid,
		BalanceIRT:      pricing.IRT(balanceIRT),
		BalanceUSDMicro: pricing.MicroUSD(balanceUSDMicro),
		Version:         version,
	}
	if floorOverride != nil {
		f := pricing.IRT(*floorOverride)
		w.FloorOverrideIRT = &f
	}
	return w, nil
}

// RecordLedgerEntry records an append-only entry in shop.wallet_ledger.
// The unique index uq_ledger_idempotency on (ref_type, ref_id, kind, currency)
// prevents double-crediting or double-debiting when ref_id is provided.
func (r *walletRepo) RecordLedgerEntry(ctx context.Context, entry *wallet.LedgerEntry) error {
	if r.s == nil || r.s.pool == nil {
		return errors.New("postgres: nil pool")
	}
	if entry == nil {
		return errors.New("postgres: nil ledger entry")
	}

	createdAt := entry.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
		entry.CreatedAt = createdAt
	} else {
		createdAt = createdAt.UTC()
	}

	id := entry.ID
	if id == "" {
		id = fmt.Sprintf("ledg-%d", time.Now().UnixNano())
		entry.ID = id
	}

	wid := entry.WalletID

	query := `
		INSERT INTO shop.wallet_ledger (
			id, wallet_id, currency, amount, kind, balance_after, ref_type, ref_id, created_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9
		)
	`
	_, err := r.s.pool.Exec(
		ctx,
		query,
		id,
		wid,
		string(entry.Currency),
		entry.Amount,
		string(entry.Kind),
		entry.BalanceAfter,
		entry.RefType,
		entry.RefID,
		createdAt,
	)
	if err != nil {
		return fmt.Errorf("postgres: record ledger entry %s: %w", id, err)
	}
	return nil
}

// UpdateBalances persists updated balances and custom floor setting to shop.wallets,
// increments the version counter in the database, and updates w.Version to reflect the new version.
func (r *walletRepo) UpdateBalances(ctx context.Context, w *wallet.Wallet) error {
	if r.s == nil || r.s.pool == nil {
		return errors.New("postgres: nil pool")
	}
	if w == nil {
		return errors.New("postgres: nil wallet")
	}

	var floorOverride *int64
	if w.FloorOverrideIRT != nil {
		val := int64(*w.FloorOverrideIRT)
		floorOverride = &val
	}

	query := `
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
	err := r.s.pool.QueryRow(
		ctx,
		query,
		int64(w.BalanceIRT),
		int64(w.BalanceUSDMicro),
		floorOverride,
		w.UserID,
	).Scan(&newVersion)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// If wallet was not yet persisted, insert it
			insertQuery := `
				INSERT INTO shop.wallets (
					user_id, id, balance_irt, balance_usd_micro, floor_override_irt, version, updated_at
				) VALUES (
					$1, $2, $3, $4, $5, $6, NOW()
				)
				RETURNING version
			`
			targetVer := w.Version + 1
			if targetVer <= 0 {
				targetVer = 1
			}
			wid := w.ID
			if wid == 0 {
				wid = w.UserID
			}
			if err := r.s.pool.QueryRow(
				ctx,
				insertQuery,
				w.UserID,
				wid,
				int64(w.BalanceIRT),
				int64(w.BalanceUSDMicro),
				floorOverride,
				targetVer,
			).Scan(&newVersion); err != nil {
				return fmt.Errorf("postgres: update balances (insert) for user %d: %w", w.UserID, err)
			}
		} else {
			return fmt.Errorf("postgres: update balances for user %d: %w", w.UserID, err)
		}
	}

	w.Version = newVersion
	return nil
}

// DebitAtomic atomically debits the given amount in IRT from the user's wallet within an
// isolated transaction (SELECT ... FOR UPDATE). It checks that the user's effective floor
// is not breached, calculates the debit split between IRT and USD micro-units, updates the
// cached balances, increments the version counter, and records the corresponding ledger entry.
func (r *walletRepo) DebitAtomic(
	ctx context.Context,
	userID int64,
	totalIRT pricing.IRT,
	segment fee.Segment,
	sellRate pricing.IRT,
	kind wallet.LedgerKind,
	refType, refID string,
) (*wallet.Wallet, error) {
	if r.s == nil || r.s.pool == nil {
		return nil, errors.New("postgres: nil pool")
	}
	if totalIRT <= 0 {
		return nil, wallet.ErrInvalidDebitAmount
	}
	if sellRate <= 0 {
		return nil, errors.New("postgres: sell rate must be positive")
	}

	tx, err := r.s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("postgres: begin debit tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Lock wallet row for update
	query := `
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
	err = tx.QueryRow(ctx, query, userID).Scan(
		&uid, &wid, &balanceIRT, &balanceUSDMicro, &floorOverride, &version,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		// Auto-create default zero-balance wallet row within tx
		insertQuery := `
			INSERT INTO shop.wallets (
				user_id, id, balance_irt, balance_usd_micro, version, updated_at
			) VALUES (
				$1, $1, 0, 0, 1, NOW()
			)
			RETURNING user_id, id, balance_irt, balance_usd_micro, floor_override_irt, version
		`
		if err := tx.QueryRow(ctx, insertQuery, userID).Scan(
			&uid, &wid, &balanceIRT, &balanceUSDMicro, &floorOverride, &version,
		); err != nil {
			return nil, fmt.Errorf("postgres: init wallet in debit tx: %w", err)
		}
	} else if err != nil {
		return nil, fmt.Errorf("postgres: lock wallet for debit: %w", err)
	}

	if wid == 0 {
		wid = uid
	}

	w := &wallet.Wallet{
		ID:              wid,
		UserID:          uid,
		BalanceIRT:      pricing.IRT(balanceIRT),
		BalanceUSDMicro: pricing.MicroUSD(balanceUSDMicro),
		Version:         version,
	}
	if floorOverride != nil {
		f := pricing.IRT(*floorOverride)
		w.FloorOverrideIRT = &f
	}

	// Verify floor before debiting
	if !w.CanPurchase(totalIRT, segment, sellRate) {
		return nil, wallet.ErrBelowFloor
	}

	// Calculate debit split: IRT first, remainder in MicroUSD
	debitIRT, debitUSD := wallet.DebitSplit(totalIRT, w.BalanceIRT, sellRate)
	w.BalanceIRT -= debitIRT
	w.BalanceUSDMicro -= debitUSD

	var floorVal *int64
	if w.FloorOverrideIRT != nil {
		val := int64(*w.FloorOverrideIRT)
		floorVal = &val
	}

	updateQuery := `
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
		updateQuery,
		int64(w.BalanceIRT),
		int64(w.BalanceUSDMicro),
		floorVal,
		userID,
	).Scan(&newVersion); err != nil {
		return nil, fmt.Errorf("postgres: update balance in debit tx: %w", err)
	}
	w.Version = newVersion

	now := time.Now().UTC()
	ledgerQuery := `
		INSERT INTO shop.wallet_ledger (
			id, wallet_id, currency, amount, kind, balance_after, ref_type, ref_id, created_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9
		)
	`
	if debitIRT > 0 {
		salt := make([]byte, 4)
		_, _ = rand.Read(salt)
		hexSalt := hex.EncodeToString(salt)
		ledgID := fmt.Sprintf("ledg-%d-%s", time.Now().UnixNano(), hexSalt)
		if _, err := tx.Exec(
			ctx,
			ledgerQuery,
			ledgID,
			w.ID,
			string(wallet.CurrencyIRT),
			-int64(debitIRT),
			string(kind),
			int64(w.BalanceIRT),
			refType,
			refID,
			now,
		); err != nil {
			return nil, fmt.Errorf("postgres: record debit irt ledger: %w", err)
		}
	}

	if debitUSD > 0 {
		salt := make([]byte, 4)
		_, _ = rand.Read(salt)
		hexSalt := hex.EncodeToString(salt)
		ledgID := fmt.Sprintf("ledg-%d-%s", time.Now().UnixNano(), hexSalt)
		if _, err := tx.Exec(
			ctx,
			ledgerQuery,
			ledgID,
			w.ID,
			string(wallet.CurrencyUSD),
			-int64(debitUSD),
			string(kind),
			int64(w.BalanceUSDMicro),
			refType,
			refID,
			now,
		); err != nil {
			return nil, fmt.Errorf("postgres: record debit usd ledger: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("postgres: commit debit tx: %w", err)
	}

	return w, nil
}
