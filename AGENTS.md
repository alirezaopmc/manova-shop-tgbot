---
read_with:
  - project-docs/docs/shop-system-design.md
  - project-docs/internal/hetzner-api-facts.md
  - project-docs/docs/usd-rate-source.md
---

# shop-tgbot (Manova Hetzner Cloud Shop Bot)

Commerce Telegram bot for selling Hetzner Cloud servers and digital products with two-currency wallet (Toman & USD), gapless per-minute billing, TOTP-gated admin operations, and phone-verified customer onboarding.

## Architecture & Principles

- **Style:** Hexagonal architecture (ports and adapters).
  - `internal/domain/`: Pure Go, zero I/O, domain entities, value types, invariant enforcement.
  - `internal/application/`: Use cases and abstract ports (`port.Repositories`, `port.CloudProvider`, `port.FxRateSource`, etc.).
  - `internal/adapters/`: Infrastructure adapters (Telegram long-polling, Hetzner API, Postgres/SQLC, Crypto, HTTP/Cloudflare Tunnel).
  - `internal/platform/`: Configuration, redacted secrets, clocks, process lifecycle.
  - `cmd/shopbot`: Runtime daemon.
  - `cmd/shopctl`: Operational CLI (migrate, catalog, reconcile, card set, totp reset).
- **Dependency Rule:** `domain` ← `application` ← `adapters` ← `cmd`. No circular imports or database/network leaks into domain.
- **Billing Invariant:** Gapless billing, integer Toman / micro-USD, reconciliation from Hetzner timestamps, exact backfill across restarts.
- **Security:**
  - Secrets loaded from root-owned environment / files (never logged, redacted in `slog`).
  - TOTP required for privileged operations (card changes, floor overrides, refund payouts).
  - Envelope encryption with HKDF-derived subkeys for sensitive data.

## Commands

```bash
# Run unit tests with race detector
go test ./... -v -race -count=1

# Vet checks
go vet ./...

# Run shopbot daemon
go run ./cmd/shopbot

# Run CLI operations
go run ./cmd/shopctl --help
```
