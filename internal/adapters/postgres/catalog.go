package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/alirezaopmc/manova-shop-tgbot/internal/domain/catalog"
	"github.com/alirezaopmc/manova-shop-tgbot/internal/domain/osimage"
	"github.com/alirezaopmc/manova-shop-tgbot/internal/domain/pricing"
)

type catalogRepo struct {
	s *Store
}

func scanCatalogItem(row pgx.Row) (*catalog.Item, error) {
	var (
		it        catalog.Item
		familyStr string
		hourlyUSD int64
		monthCap  int64
	)
	err := row.Scan(
		&it.ID,
		&it.Name,
		&familyStr,
		&it.VCPU,
		&it.RAMGB,
		&it.DiskGB,
		&it.HetznerType,
		&hourlyUSD,
		&monthCap,
		&it.Active,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	it.Family = catalog.ServerFamily(familyStr)
	it.HourlyUSDMicro = pricing.MicroUSD(hourlyUSD)
	it.MonthlyCapUSD = pricing.MicroUSD(monthCap)
	return &it, nil
}

// GetItem retrieves a single catalog item by ID.
func (r *catalogRepo) GetItem(ctx context.Context, itemID string) (*catalog.Item, error) {
	if r.s == nil || r.s.pool == nil {
		return nil, errors.New("postgres: nil pool")
	}
	if itemID == "" {
		return nil, errors.New("postgres: empty item ID")
	}

	query := `
		SELECT id, name, family, vcpu, ram_gb, disk_gb, hetzner_type,
		       hourly_usd_micro, monthly_cap_usd, active
		FROM shop.catalog_items
		WHERE id = $1
	`
	it, err := scanCatalogItem(r.s.pool.QueryRow(ctx, query, itemID))
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("postgres: get catalog item %s: %w", itemID, err)
	}
	return it, nil
}

// ListActiveItems retrieves all active catalog items sorted by price ascending.
func (r *catalogRepo) ListActiveItems(ctx context.Context) ([]catalog.Item, error) {
	if r.s == nil || r.s.pool == nil {
		return nil, errors.New("postgres: nil pool")
	}

	query := `
		SELECT id, name, family, vcpu, ram_gb, disk_gb, hetzner_type,
		       hourly_usd_micro, monthly_cap_usd, active
		FROM shop.catalog_items
		WHERE active = true
		ORDER BY hourly_usd_micro ASC, id ASC
	`
	rows, err := r.s.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("postgres: list active catalog items: %w", err)
	}
	defer rows.Close()

	out := make([]catalog.Item, 0)
	for rows.Next() {
		it, err := scanCatalogItem(rows)
		if err != nil {
			return nil, fmt.Errorf("postgres: scan catalog item: %w", err)
		}
		out = append(out, *it)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: iterate active catalog items: %w", err)
	}
	return out, nil
}

// --- OSCatalogRepository implementation ---

type osCatalogRepo struct {
	s *Store
}

func scanOSEntry(row pgx.Row) (*osimage.Entry, error) {
	var (
		e         osimage.Entry
		distroStr string
		archStr   string
		statusStr string
	)
	err := row.Scan(
		&e.ID,
		&distroStr,
		&e.Version,
		&archStr,
		&e.ImageName,
		&statusStr,
		&e.UnavailableAfter,
		&e.DisplayNameFA,
		&e.DisplayNameEN,
		&e.SortOrder,
		&e.SyncedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	e.Distribution = osimage.Distribution(distroStr)
	e.Architecture = osimage.Architecture(archStr)
	e.HetznerStatus = osimage.Status(statusStr)
	if e.UnavailableAfter != nil {
		t := e.UnavailableAfter.UTC()
		e.UnavailableAfter = &t
	}
	e.SyncedAt = e.SyncedAt.UTC()
	return &e, nil
}

// GetOffered returns all available OS image entries matching optional distro and arch filters.
func (r *osCatalogRepo) GetOffered(ctx context.Context, distro osimage.Distribution, arch osimage.Architecture) ([]osimage.Entry, error) {
	if r.s == nil || r.s.pool == nil {
		return nil, errors.New("postgres: nil pool")
	}

	now := time.Now().UTC()
	query := `
		SELECT id, distribution, version, architecture, image_name,
		       hetzner_status, unavailable_after, display_name_fa, display_name_en,
		       sort_order, synced_at
		FROM shop.os_catalog_entries
		WHERE hetzner_status = 'available'
		  AND (unavailable_after IS NULL OR unavailable_after > $1)
		  AND ($2 = '' OR distribution = $2)
		  AND ($3 = '' OR architecture = $3)
		ORDER BY sort_order ASC, id ASC
	`
	rows, err := r.s.pool.Query(ctx, query, now, string(distro), string(arch))
	if err != nil {
		return nil, fmt.Errorf("postgres: get offered os images: %w", err)
	}
	defer rows.Close()

	out := make([]osimage.Entry, 0)
	for rows.Next() {
		e, err := scanOSEntry(rows)
		if err != nil {
			return nil, fmt.Errorf("postgres: scan os catalog entry: %w", err)
		}
		out = append(out, *e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: iterate offered os images: %w", err)
	}
	return out, nil
}

// ReplaceSnapshot atomically clears and replaces the entire OS image catalog snapshot.
func (r *osCatalogRepo) ReplaceSnapshot(ctx context.Context, entries []osimage.Entry, syncedAt time.Time) error {
	if r.s == nil || r.s.pool == nil {
		return errors.New("postgres: nil pool")
	}

	if syncedAt.IsZero() {
		syncedAt = time.Now().UTC()
	} else {
		syncedAt = syncedAt.UTC()
	}

	tx, err := r.s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("postgres: begin replace os snapshot tx: %w", err)
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, "DELETE FROM shop.os_catalog_entries"); err != nil {
		return fmt.Errorf("postgres: delete old os entries: %w", err)
	}

	insertQuery := `
		INSERT INTO shop.os_catalog_entries (
			id, distribution, version, architecture, image_name,
			hetzner_status, unavailable_after, display_name_fa, display_name_en,
			sort_order, synced_at
		) VALUES (
			$1, $2, $3, $4, $5,
			$6, $7, $8, $9,
			$10, $11
		)
	`
	for _, e := range entries {
		entrySyncedAt := e.SyncedAt
		if entrySyncedAt.IsZero() {
			entrySyncedAt = syncedAt
		} else {
			entrySyncedAt = entrySyncedAt.UTC()
		}
		status := string(e.HetznerStatus)
		if status == "" {
			status = string(osimage.StatusAvailable)
		}
		var unavail *time.Time
		if e.UnavailableAfter != nil {
			t := e.UnavailableAfter.UTC()
			unavail = &t
		}

		if _, err := tx.Exec(ctx, insertQuery,
			e.ID,
			string(e.Distribution),
			e.Version,
			string(e.Architecture),
			e.ImageName,
			status,
			unavail,
			e.DisplayNameFA,
			e.DisplayNameEN,
			e.SortOrder,
			entrySyncedAt,
		); err != nil {
			return fmt.Errorf("postgres: insert os entry %s: %w", e.ID, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("postgres: commit replace os snapshot tx: %w", err)
	}
	return nil
}
