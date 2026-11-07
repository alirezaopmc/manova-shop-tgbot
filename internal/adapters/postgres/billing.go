package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/alirezaopmc/manova-shop-tgbot/internal/domain/billing"
	"github.com/alirezaopmc/manova-shop-tgbot/internal/domain/pricing"
)

type billingRepo struct {
	s *Store
}

func scanResource(row pgx.Row) (*billing.ProvisionedResource, error) {
	var (
		res           billing.ProvisionedResource
		statusStr     string
		powerStateStr string
		hourlyUSD     int64
		monthlyCap    int64
	)
	err := row.Scan(
		&res.ID,
		&res.UserID,
		&res.ServerName,
		&res.OrderItemID,
		&res.ExternalID,
		&statusStr,
		&powerStateStr,
		&hourlyUSD,
		&monthlyCap,
		&res.IPv4Address,
		&res.IPv6Address,
		&res.Location,
		&res.ImageName,
		&res.RootPassword,
		&res.SSHKey,
		&res.BillingStartAt,
		&res.BillingEndAt,
		&res.PostedThroughMinute,
		&res.RemainderMicroIRT,
		&res.LastVerifiedAt,
		&res.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	res.Status = billing.ResourceStatus(statusStr)
	res.PowerState = billing.PowerState(powerStateStr)
	res.HourlyPriceUSD = pricing.MicroUSD(hourlyUSD)
	res.MonthlyCapUSD = pricing.MicroUSD(monthlyCap)
	res.BillingStartAt = res.BillingStartAt.UTC()
	if res.BillingEndAt != nil {
		t := res.BillingEndAt.UTC()
		res.BillingEndAt = &t
	}
	res.LastVerifiedAt = res.LastVerifiedAt.UTC()
	res.CreatedAt = res.CreatedAt.UTC()
	return &res, nil
}

// GetResource retrieves a provisioned resource record by its unique ID.
func (r *billingRepo) GetResource(ctx context.Context, resourceID string) (*billing.ProvisionedResource, error) {
	if r.s == nil || r.s.pool == nil {
		return nil, errors.New("postgres: nil pool")
	}
	if resourceID == "" {
		return nil, errors.New("postgres: empty resource ID")
	}

	query := `
		SELECT id, user_id, server_name, order_item_id, external_id,
		       status, power_state, hourly_price_usd, monthly_cap_usd,
		       ipv4_address, ipv6_address, location, image_name,
		       root_password, ssh_key, billing_start_at, billing_end_at,
		       posted_through_minute, remainder_micro_irt, last_verified_at, created_at
		FROM shop.provisioned_resources
		WHERE id = $1
	`
	res, err := scanResource(r.s.pool.QueryRow(ctx, query, resourceID))
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("postgres: get resource by id %s: %w", resourceID, err)
	}
	return res, nil
}

// ListActiveResources lists all currently active provisioned resources across all users.
func (r *billingRepo) ListActiveResources(ctx context.Context) ([]billing.ProvisionedResource, error) {
	if r.s == nil || r.s.pool == nil {
		return nil, errors.New("postgres: nil pool")
	}

	query := `
		SELECT id, user_id, server_name, order_item_id, external_id,
		       status, power_state, hourly_price_usd, monthly_cap_usd,
		       ipv4_address, ipv6_address, location, image_name,
		       root_password, ssh_key, billing_start_at, billing_end_at,
		       posted_through_minute, remainder_micro_irt, last_verified_at, created_at
		FROM shop.provisioned_resources
		WHERE status = $1
		ORDER BY created_at ASC
	`
	rows, err := r.s.pool.Query(ctx, query, string(billing.ResourceStatusActive))
	if err != nil {
		return nil, fmt.Errorf("postgres: list active resources: %w", err)
	}
	defer rows.Close()

	out := make([]billing.ProvisionedResource, 0)
	for rows.Next() {
		res, err := scanResource(rows)
		if err != nil {
			return nil, fmt.Errorf("postgres: scan active resource: %w", err)
		}
		out = append(out, *res)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: iterate active resources: %w", err)
	}
	return out, nil
}

// ListUserResources lists all currently active provisioned resources belonging to a user.
func (r *billingRepo) ListUserResources(ctx context.Context, userID int64) ([]billing.ProvisionedResource, error) {
	if r.s == nil || r.s.pool == nil {
		return nil, errors.New("postgres: nil pool")
	}

	query := `
		SELECT id, user_id, server_name, order_item_id, external_id,
		       status, power_state, hourly_price_usd, monthly_cap_usd,
		       ipv4_address, ipv6_address, location, image_name,
		       root_password, ssh_key, billing_start_at, billing_end_at,
		       posted_through_minute, remainder_micro_irt, last_verified_at, created_at
		FROM shop.provisioned_resources
		WHERE user_id = $1 AND status = $2
		ORDER BY created_at ASC
	`
	rows, err := r.s.pool.Query(ctx, query, userID, string(billing.ResourceStatusActive))
	if err != nil {
		return nil, fmt.Errorf("postgres: list user resources (user %d): %w", userID, err)
	}
	defer rows.Close()

	out := make([]billing.ProvisionedResource, 0)
	for rows.Next() {
		res, err := scanResource(rows)
		if err != nil {
			return nil, fmt.Errorf("postgres: scan user resource: %w", err)
		}
		out = append(out, *res)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: iterate user resources: %w", err)
	}
	return out, nil
}

// SaveResource inserts or updates a provisioned resource record.
func (r *billingRepo) SaveResource(ctx context.Context, res *billing.ProvisionedResource) error {
	if r.s == nil || r.s.pool == nil {
		return errors.New("postgres: nil pool")
	}
	if res == nil {
		return errors.New("postgres: nil resource")
	}
	if res.ID == "" {
		return errors.New("postgres: resource ID cannot be empty")
	}
	if res.UserID <= 0 {
		return errors.New("postgres: invalid resource user ID")
	}

	now := time.Now().UTC()
	startAt := res.BillingStartAt
	if startAt.IsZero() {
		startAt = now
	}
	verifiedAt := res.LastVerifiedAt
	if verifiedAt.IsZero() {
		verifiedAt = now
	}
	createdAt := res.CreatedAt
	if createdAt.IsZero() {
		createdAt = now
	}
	status := res.Status
	if status == "" {
		status = billing.ResourceStatusActive
	}
	powerState := res.PowerState
	if powerState == "" {
		powerState = billing.PowerStateRunning
	}

	query := `
		INSERT INTO shop.provisioned_resources (
			id, user_id, server_name, order_item_id, external_id,
			status, power_state, hourly_price_usd, monthly_cap_usd,
			ipv4_address, ipv6_address, location, image_name,
			root_password, ssh_key, billing_start_at, billing_end_at,
			posted_through_minute, remainder_micro_irt, last_verified_at, created_at
		) VALUES (
			$1, $2, $3, $4, $5,
			$6, $7, $8, $9,
			$10, $11, $12, $13,
			$14, $15, $16, $17,
			$18, $19, $20, $21
		)
		ON CONFLICT (id) DO UPDATE SET
			user_id = EXCLUDED.user_id,
			server_name = EXCLUDED.server_name,
			order_item_id = EXCLUDED.order_item_id,
			external_id = EXCLUDED.external_id,
			status = EXCLUDED.status,
			power_state = EXCLUDED.power_state,
			hourly_price_usd = EXCLUDED.hourly_price_usd,
			monthly_cap_usd = EXCLUDED.monthly_cap_usd,
			ipv4_address = EXCLUDED.ipv4_address,
			ipv6_address = EXCLUDED.ipv6_address,
			location = EXCLUDED.location,
			image_name = EXCLUDED.image_name,
			root_password = EXCLUDED.root_password,
			ssh_key = EXCLUDED.ssh_key,
			billing_start_at = EXCLUDED.billing_start_at,
			billing_end_at = EXCLUDED.billing_end_at,
			posted_through_minute = EXCLUDED.posted_through_minute,
			remainder_micro_irt = EXCLUDED.remainder_micro_irt,
			last_verified_at = EXCLUDED.last_verified_at
	`
	_, err := r.s.pool.Exec(ctx, query,
		res.ID,
		res.UserID,
		res.ServerName,
		res.OrderItemID,
		res.ExternalID,
		string(status),
		string(powerState),
		int64(res.HourlyPriceUSD),
		int64(res.MonthlyCapUSD),
		res.IPv4Address,
		res.IPv6Address,
		res.Location,
		res.ImageName,
		res.RootPassword,
		res.SSHKey,
		startAt,
		res.BillingEndAt,
		res.PostedThroughMinute,
		res.RemainderMicroIRT,
		verifiedAt,
		createdAt,
	)
	if err != nil {
		return fmt.Errorf("postgres: save resource %s: %w", res.ID, err)
	}
	return nil
}

// SaveMinutePosting inserts a single minute charge record into shop.minute_postings.
func (r *billingRepo) SaveMinutePosting(ctx context.Context, p *billing.MinutePosting) error {
	if r.s == nil || r.s.pool == nil {
		return errors.New("postgres: nil pool")
	}
	if p == nil {
		return errors.New("postgres: nil minute posting")
	}
	if p.ResourceID == "" {
		return errors.New("postgres: empty resource ID in minute posting")
	}
	if p.MinuteIndex < 0 {
		return billing.ErrInvalidMinuteIndex
	}

	id := p.ID
	if id == "" {
		id = fmt.Sprintf("mp-%s-%d", p.ResourceID, p.MinuteIndex)
	}
	state := p.State
	if state == "" {
		state = "posted"
	}
	createdAt := p.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}

	query := `
		INSERT INTO shop.minute_postings (
			id, resource_id, minute_index, state,
			usd_micro, amount_irt, remainder_micro_irt,
			rate_obs_id, ledger_id, created_at
		) VALUES (
			$1, $2, $3, $4,
			$5, $6, $7,
			$8, $9, $10
		)
	`
	_, err := r.s.pool.Exec(ctx, query,
		id,
		p.ResourceID,
		p.MinuteIndex,
		state,
		int64(p.USDMicro),
		int64(p.AmountIRT),
		p.RemainderMicroIRT,
		p.RateObsID,
		p.LedgerID,
		createdAt,
	)
	if err != nil {
		return fmt.Errorf("postgres: save minute posting (%s, minute %d): %w", p.ResourceID, p.MinuteIndex, err)
	}
	return nil
}

// UpdateResourceBillingEnd updates billing termination timestamp and lifecycle status of a resource.
func (r *billingRepo) UpdateResourceBillingEnd(ctx context.Context, resourceID string, billingEnd time.Time, status billing.ResourceStatus) error {
	if r.s == nil || r.s.pool == nil {
		return errors.New("postgres: nil pool")
	}
	if resourceID == "" {
		return errors.New("postgres: empty resource ID")
	}

	query := `
		UPDATE shop.provisioned_resources
		SET billing_end_at = $2,
		    status = $3
		WHERE id = $1
	`
	cmd, err := r.s.pool.Exec(ctx, query, resourceID, billingEnd.UTC(), string(status))
	if err != nil {
		return fmt.Errorf("postgres: update resource billing end (%s): %w", resourceID, err)
	}
	if cmd.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
