-- +goose Up
-- Schema definition for manova-shop-tgbot persistent storage.
CREATE SCHEMA IF NOT EXISTS shop;

-- shop.users stores customer and administrator profile facts.
CREATE TABLE IF NOT EXISTS shop.users (
    telegram_user_id BIGINT PRIMARY KEY,
    username TEXT NOT NULL DEFAULT '',
    first_name TEXT NOT NULL DEFAULT '',
    last_name TEXT NOT NULL DEFAULT '',
    role TEXT NOT NULL DEFAULT 'USER',
    locale TEXT NOT NULL DEFAULT 'fa',
    segment TEXT NOT NULL DEFAULT 'new',
    invited_by BIGINT,
    invite_code TEXT,
    first_purchase_state TEXT NOT NULL DEFAULT 'eligible',
    phone_verified_at TIMESTAMPTZ,
    phone_e164 TEXT,
    phone_hash TEXT,
    blocked BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS users_phone_hash_idx ON shop.users (phone_hash) WHERE phone_hash IS NOT NULL;
CREATE INDEX IF NOT EXISTS users_username_idx ON shop.users (username) WHERE username != '';

-- shop.invites models onboarding tokens created by administrators.
CREATE TABLE IF NOT EXISTS shop.invites (
    code TEXT PRIMARY KEY,
    created_by BIGINT NOT NULL,
    max_uses INT NOT NULL DEFAULT 1,
    uses_count INT NOT NULL DEFAULT 0,
    expires_at TIMESTAMPTZ,
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS invites_created_by_idx ON shop.invites (created_by);

-- shop.invite_claims records individual claims of an invite token by users to prevent duplicates.
CREATE TABLE IF NOT EXISTS shop.invite_claims (
    invite_code TEXT NOT NULL REFERENCES shop.invites(code) ON DELETE CASCADE,
    user_id BIGINT NOT NULL,
    claimed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (invite_code, user_id)
);

CREATE INDEX IF NOT EXISTS invite_claims_user_id_idx ON shop.invite_claims (user_id);

-- shop.wallets caches current user balances and credit floor settings.
CREATE TABLE IF NOT EXISTS shop.wallets (
    user_id BIGINT PRIMARY KEY,
    id BIGINT NOT NULL DEFAULT 0,
    balance_irt BIGINT NOT NULL DEFAULT 0,
    balance_usd_micro BIGINT NOT NULL DEFAULT 0,
    floor_override_irt BIGINT,
    version BIGINT NOT NULL DEFAULT 1,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- shop.wallet_ledger is the append-only audit trail for all financial balance mutations.
CREATE TABLE IF NOT EXISTS shop.wallet_ledger (
    id TEXT PRIMARY KEY,
    wallet_id BIGINT NOT NULL,
    currency TEXT NOT NULL,
    amount BIGINT NOT NULL,
    kind TEXT NOT NULL,
    balance_after BIGINT NOT NULL,
    ref_type TEXT NOT NULL DEFAULT '',
    ref_id TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS wallet_ledger_wallet_id_idx ON shop.wallet_ledger (wallet_id, created_at);
CREATE INDEX IF NOT EXISTS wallet_ledger_ref_idx ON shop.wallet_ledger (ref_type, ref_id) WHERE ref_id != '';

-- Trigger enforcing append-only immutability on wallet_ledger.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION shop.prevent_ledger_mutation()
RETURNS TRIGGER AS $$
BEGIN
    RAISE EXCEPTION 'wallet_ledger rows are immutable: UPDATE and DELETE are prohibited';
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

DROP TRIGGER IF EXISTS wallet_ledger_no_update ON shop.wallet_ledger;
CREATE TRIGGER wallet_ledger_no_update
    BEFORE UPDATE ON shop.wallet_ledger
    FOR EACH ROW
    EXECUTE FUNCTION shop.prevent_ledger_mutation();

DROP TRIGGER IF EXISTS wallet_ledger_no_delete ON shop.wallet_ledger;
CREATE TRIGGER wallet_ledger_no_delete
    BEFORE DELETE ON shop.wallet_ledger
    FOR EACH ROW
    EXECUTE FUNCTION shop.prevent_ledger_mutation();

-- shop.catalog_items defines sellable cloud compute configurations.
CREATE TABLE IF NOT EXISTS shop.catalog_items (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    family TEXT NOT NULL,
    vcpu INT NOT NULL,
    ram_gb INT NOT NULL,
    disk_gb INT NOT NULL,
    hetzner_type TEXT NOT NULL,
    hourly_usd_micro BIGINT NOT NULL,
    monthly_cap_usd BIGINT NOT NULL,
    active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- shop.os_catalog_entries holds synced available operating system images.
CREATE TABLE IF NOT EXISTS shop.os_catalog_entries (
    id TEXT PRIMARY KEY,
    distribution TEXT NOT NULL,
    version TEXT NOT NULL,
    architecture TEXT NOT NULL,
    image_name TEXT NOT NULL,
    hetzner_status TEXT NOT NULL DEFAULT 'available',
    unavailable_after TIMESTAMPTZ,
    display_name_fa TEXT NOT NULL,
    display_name_en TEXT NOT NULL,
    sort_order INT NOT NULL DEFAULT 0,
    synced_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS os_catalog_entries_lookup_idx ON shop.os_catalog_entries (distribution, architecture, sort_order);

-- shop.orders holds server purchase and subscription orders.
CREATE TABLE IF NOT EXISTS shop.orders (
    id TEXT PRIMARY KEY,
    user_id BIGINT NOT NULL,
    status TEXT NOT NULL,
    checkout_mode TEXT NOT NULL,
    hold_reason TEXT,
    quote_id TEXT NOT NULL DEFAULT '',
    locked_quote JSONB NOT NULL DEFAULT '{}'::jsonb,
    checkout_credit_irt BIGINT,
    card_account_id TEXT,
    policy_version TEXT NOT NULL DEFAULT '',
    policy_accepted_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at TIMESTAMPTZ NOT NULL,
    version BIGINT NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS orders_user_id_idx ON shop.orders (user_id);
CREATE INDEX IF NOT EXISTS orders_status_idx ON shop.orders (status);

-- shop.order_events records order state machine transition history.
CREATE TABLE IF NOT EXISTS shop.order_events (
    id TEXT PRIMARY KEY,
    order_id TEXT NOT NULL REFERENCES shop.orders(id) ON DELETE CASCADE,
    from_status TEXT NOT NULL,
    to_status TEXT NOT NULL,
    source TEXT NOT NULL,
    source_ref TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS order_events_order_id_idx ON shop.order_events (order_id, created_at);

-- shop.receipts records bank card-to-card transfer payment proofs submitted for manual or automated verification.
CREATE TABLE IF NOT EXISTS shop.receipts (
    id TEXT PRIMARY KEY,
    user_id BIGINT NOT NULL,
    purpose TEXT NOT NULL,
    order_id TEXT,
    quote_id TEXT,
    amount_irr BIGINT NOT NULL,
    amount_irt BIGINT NOT NULL,
    tracking_code_norm TEXT NOT NULL,
    image_sha256 TEXT NOT NULL,
    image_ref TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL,
    decided_by_admin_id BIGINT,
    decided_at TIMESTAMPTZ,
    reject_reason TEXT,
    note TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS receipts_image_sha256_idx ON shop.receipts (image_sha256);
CREATE UNIQUE INDEX IF NOT EXISTS receipts_tracking_code_norm_idx ON shop.receipts (tracking_code_norm);
CREATE INDEX IF NOT EXISTS receipts_user_id_idx ON shop.receipts (user_id);
CREATE INDEX IF NOT EXISTS receipts_status_idx ON shop.receipts (status);

-- shop.provisioned_resources tracks active and decommissioned Hetzner Cloud virtual servers.
CREATE TABLE IF NOT EXISTS shop.provisioned_resources (
    id TEXT PRIMARY KEY,
    user_id BIGINT NOT NULL,
    server_name TEXT NOT NULL,
    order_item_id TEXT NOT NULL,
    external_id TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL,
    power_state TEXT NOT NULL,
    hourly_price_usd BIGINT NOT NULL,
    monthly_cap_usd BIGINT NOT NULL,
    ipv4_address TEXT NOT NULL DEFAULT '',
    ipv6_address TEXT NOT NULL DEFAULT '',
    location TEXT NOT NULL DEFAULT '',
    image_name TEXT NOT NULL DEFAULT '',
    root_password TEXT NOT NULL DEFAULT '',
    ssh_key TEXT NOT NULL DEFAULT '',
    billing_start_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    billing_end_at TIMESTAMPTZ,
    posted_through_minute BIGINT NOT NULL DEFAULT 0,
    remainder_micro_irt BIGINT NOT NULL DEFAULT 0,
    last_verified_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS provisioned_resources_user_id_idx ON shop.provisioned_resources (user_id);
CREATE INDEX IF NOT EXISTS provisioned_resources_status_idx ON shop.provisioned_resources (status);
CREATE INDEX IF NOT EXISTS provisioned_resources_external_id_idx ON shop.provisioned_resources (external_id) WHERE external_id != '';

-- shop.minute_postings records per-minute billing deductions for active cloud servers.
CREATE TABLE IF NOT EXISTS shop.minute_postings (
    id TEXT PRIMARY KEY,
    resource_id TEXT NOT NULL REFERENCES shop.provisioned_resources(id) ON DELETE CASCADE,
    minute_index BIGINT NOT NULL,
    state TEXT NOT NULL DEFAULT 'posted',
    usd_micro BIGINT NOT NULL,
    amount_irt BIGINT NOT NULL,
    remainder_micro_irt BIGINT NOT NULL DEFAULT 0,
    rate_obs_id TEXT NOT NULL DEFAULT '',
    ledger_id TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS minute_postings_resource_minute_idx ON shop.minute_postings (resource_id, minute_index);
CREATE INDEX IF NOT EXISTS minute_postings_resource_id_idx ON shop.minute_postings (resource_id);

-- shop.refund_requests records user refund claims and approval / payout workflow states.
CREATE TABLE IF NOT EXISTS shop.refund_requests (
    id TEXT PRIMARY KEY,
    order_id TEXT NOT NULL,
    trigger TEXT NOT NULL,
    base_irt BIGINT NOT NULL,
    commission_irt BIGINT NOT NULL,
    payout_irt BIGINT NOT NULL,
    card_last4 TEXT NOT NULL,
    status TEXT NOT NULL,
    requested_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    approved_by_admin_id BIGINT,
    approved_at TIMESTAMPTZ,
    rejected_by_admin_id BIGINT,
    rejected_at TIMESTAMPTZ,
    reject_reason TEXT,
    bank_reference TEXT,
    paid_out_by_admin_id BIGINT,
    paid_out_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS refund_requests_order_id_idx ON shop.refund_requests (order_id);
CREATE INDEX IF NOT EXISTS refund_requests_status_idx ON shop.refund_requests (status);

-- shop.conversation_states persists in-flight Telegram conversational wizards across bot restarts.
CREATE TABLE IF NOT EXISTS shop.conversation_states (
    user_id BIGINT NOT NULL,
    flow TEXT NOT NULL,
    state JSONB NOT NULL DEFAULT '{}'::jsonb,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_id, flow)
);

-- shop.admin_actions records audit logs for administrative interventions.
CREATE TABLE IF NOT EXISTS shop.admin_actions (
    id TEXT PRIMARY KEY,
    admin_id BIGINT NOT NULL,
    action TEXT NOT NULL,
    target_type TEXT NOT NULL,
    target_id TEXT NOT NULL,
    payload JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS admin_actions_admin_id_idx ON shop.admin_actions (admin_id);
CREATE INDEX IF NOT EXISTS admin_actions_target_idx ON shop.admin_actions (target_type, target_id);

-- Seed baseline catalog items (CX23, CX33, CX43)
INSERT INTO shop.catalog_items (id, name, family, vcpu, ram_gb, disk_gb, hetzner_type, hourly_usd_micro, monthly_cap_usd, active)
VALUES
    ('cx23', 'CX23', 'CX', 2, 4, 40, 'cx23', 7000, 4490000, true),
    ('cx33', 'CX33', 'CX', 4, 8, 80, 'cx33', 13000, 8490000, true),
    ('cx43', 'CX43', 'CX', 8, 16, 160, 'cx43', 25000, 15990000, true)
ON CONFLICT (id) DO NOTHING;

-- Seed baseline OS catalog entries (Ubuntu 24.04, Ubuntu 22.04, Debian 12)
INSERT INTO shop.os_catalog_entries (id, distribution, version, architecture, image_name, hetzner_status, display_name_fa, display_name_en, sort_order, synced_at)
VALUES
    ('ubuntu-2404', 'ubuntu', '24.04', 'x86', 'ubuntu-24.04', 'available', 'اوبونتو ۲۴.۰۴ (Ubuntu 24.04 LTS)', 'Ubuntu 24.04 LTS', 1, NOW()),
    ('ubuntu-2204', 'ubuntu', '22.04', 'x86', 'ubuntu-22.04', 'available', 'اوبونتو ۲۲.۰۴ (Ubuntu 22.04 LTS)', 'Ubuntu 22.04 LTS', 2, NOW()),
    ('debian-12', 'debian', '12', 'x86', 'debian-12', 'available', 'دبیان ۱۲ (Debian 12 Bookworm)', 'Debian 12 Bookworm', 3, NOW())
ON CONFLICT (id) DO NOTHING;

-- +goose Down
DROP TRIGGER IF EXISTS wallet_ledger_no_delete ON shop.wallet_ledger;
DROP TRIGGER IF EXISTS wallet_ledger_no_update ON shop.wallet_ledger;
DROP FUNCTION IF EXISTS shop.prevent_ledger_mutation();

DROP TABLE IF EXISTS shop.admin_actions;
DROP TABLE IF EXISTS shop.conversation_states;
DROP TABLE IF EXISTS shop.refund_requests;
DROP TABLE IF EXISTS shop.minute_postings;
DROP TABLE IF EXISTS shop.provisioned_resources;
DROP TABLE IF EXISTS shop.receipts;
DROP TABLE IF EXISTS shop.order_events;
DROP TABLE IF EXISTS shop.orders;
DROP TABLE IF EXISTS shop.os_catalog_entries;
DROP TABLE IF EXISTS shop.catalog_items;
DROP TABLE IF EXISTS shop.wallet_ledger;
DROP TABLE IF EXISTS shop.wallets;
DROP TABLE IF EXISTS shop.invite_claims;
DROP TABLE IF EXISTS shop.invites;
DROP TABLE IF EXISTS shop.users;
DROP SCHEMA IF EXISTS shop CASCADE;
