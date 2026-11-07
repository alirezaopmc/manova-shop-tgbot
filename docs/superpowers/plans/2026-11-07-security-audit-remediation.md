# Security Audit Remediation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Eliminate all critical and high-severity security vulnerabilities identified in `clients-manova-shop-tgbot-security-audit.md`, including BOLA/IDOR server hijacking, Telegram HTML injection, hardcoded secret fallbacks, recurring background billing omission, cleartext credential delivery, and bypassed TOTP authentication.

**Architecture:** Enforce strict ownership gates across all callback handlers in `servers_mgmt.go`, implement universal HTML escaping on user-supplied variables, remove fallback administrative IDs, add an in-daemon recurring minute-by-minute billing engine with automated runway protection, envelope-encrypt credentials with web one-time reveal, and gate privileged administrative actions with RFC 6238 TOTP challenges.

**Tech Stack:** Go 1.27.3, PostgreSQL 18.4, `github.com/jackc/pgx/v5`, Telego, RFC 6238 TOTP (`github.com/pquerna/otp`).

## Global Constraints
- Target repository: `clients/manova/shop-tgbot`.
- Zero direct push to `main` (ADR-026); all changes must land on a feature branch (`feat/security-hardening`) and squash-merge via GitHub PR.
- Accounting invariant must hold strictly: $\text{Balance} \equiv \sum \text{Ledger Amounts}$.
- All user-controlled strings rendered in `telego.ModeHTML` MUST be escaped with `html.EscapeString()`.
- Server lifecycle mutations MUST verify ownership (`res.UserID == userID || usr.Role == user.RoleAdmin`).
- Maintain documentation and docstring integrity.

---

### Task 1: Eliminate BOLA / IDOR Vulnerabilities in Server Management

**Files:**
- Modify: `clients/manova/shop-tgbot/internal/adapters/telegram/servers_mgmt.go`
- Test: `clients/manova/shop-tgbot/internal/adapters/telegram/servers_mgmt_test.go`

**Interfaces:**
- Invariant: A user can NEVER view, power on/off, reboot, reset password, enable rescue, or delete a server that belongs to another user. Admins (`RoleAdmin`) retain management access.

- [ ] **Step 1: Write failing BOLA authorization tests**
Write tests in `servers_mgmt_test.go` attempting to trigger each action on victim server ID using attacker user ID. Verify all return access denied and do not execute provider actions.

- [ ] **Step 2: Add ownership validation helper**
In `servers_mgmt.go`:
```go
func (s *Service) checkServerOwnership(ctx context.Context, chatID int64, userID int64, res *billing.ProvisionedResource) (bool, error) {
    if res.UserID == userID {
        return true, nil
    }
    usr, err := s.store.Users().GetByID(ctx, userID)
    if err == nil && usr != nil && usr.Role == user.RoleAdmin {
        return true, nil
    }
    _ = s.sendAccessDenied(ctx, chatID)
    return false, nil
}
```

- [ ] **Step 3: Enforce ownership checks across all handlers**
Apply check at the beginning of:
- `handleServerPowerOn`
- `handleServerPowerOff`
- `handleServerReboot`
- `handleServerResetPassword`
- `handleServerRescue`
- `handleServerDeletePrompt`
- `handleServerDeleteConfirm`

- [ ] **Step 4: Run tests to verify BOLA protection passes**
Run: `go test -C clients/manova/shop-tgbot -race -v ./internal/adapters/telegram/...`
Commit: `fix(security): eliminate BOLA and IDOR across server management actions`

---

### Task 2: Universal Telegram HTML Sanitization & Update Loop Panic Recovery

**Files:**
- Modify: `clients/manova/shop-tgbot/internal/adapters/telegram/bot.go`
- Modify: `clients/manova/shop-tgbot/internal/adapters/telegram/buy_flow.go`
- Modify: `clients/manova/shop-tgbot/internal/adapters/telegram/servers_mgmt.go`
- Test: `clients/manova/shop-tgbot/internal/adapters/telegram/html_sanitize_test.go`

**Interfaces:**
- Guarantees: Usernames, names, captions, server names, and error messages containing HTML special characters (`<`, `>`, `&`, `"`) cannot break Telegram entity parsing or inject malicious tags.

- [ ] **Step 1: Write failing tests for HTML injection and DoS vectors**
Test captions containing `<script>`, `<b unclosed`, and malicious URLs in receipt uploads, user settings, and admin detail views.

- [ ] **Step 2: Add HTML escaping helper and apply to all user-derived variables**
Sanitize:
- `msg.From.Username`, `msg.From.FirstName`, `msg.From.LastName`
- `msg.Caption` in receipt forwards
- `res.ServerName`, `res.Location`, `res.ImageName`
- `err.Error()` in provider failure messages

- [ ] **Step 3: Add panic recovery to update polling loop**
In `bot.go:Start`:
Wrap each update processing in `defer func() { if r := recover(); r != nil { slog.Error("recovered from update panic", "panic", r) } }()` so malformed updates cannot crash the daemon.

- [ ] **Step 4: Run tests and commit**
Run: `go test -C clients/manova/shop-tgbot -race -v ./internal/adapters/telegram/...`
Commit: `fix(security): sanitize all user strings in HTML templates and add panic recovery`

---

### Task 3: Secret Hardening, Template Sanitization & Strict Config Fallbacks

**Files:**
- Modify: `clients/manova/shop-tgbot/internal/platform/config/config.go`
- Modify: `clients/manova/shop-tgbot/internal/platform/config/config_test.go`
- Modify: `clients/manova/shop-tgbot/internal/adapters/hetzner/client.go`
- Modify: `clients/manova/shop-tgbot/.env.example`
- Modify: `clients/manova/shop-tgbot/deploy/systemd/shopbot.env.example`

**Interfaces:**
- `Config.ValidateProduction()` requires `ADMIN_TELEGRAM_ID`.
- Hetzner client wraps token in `secrets.Secret[string]`.
- Public templates scrub private IDs and names.

- [ ] **Step 1: Remove hardcoded admin ID and cardholder fallbacks in config.go**
Remove default `6246642516` and `"Alireza Jafartash"`. If unset, require them in `ValidateProduction()`.

- [ ] **Step 2: Scrub sensitive IDs from public example templates**
Replace with generic `<admin_telegram_id>` and `<card_holder_name>`.

- [ ] **Step 3: Harden Hetzner client**
- Wrap `Token` in `secrets.Secret[string]`.
- Validate `externalID` matches digits (`^[0-9]+$`) to prevent path traversal.

- [ ] **Step 4: Run tests and commit**
Run: `go test -C clients/manova/shop-tgbot -race -v ./internal/platform/config/... ./internal/adapters/hetzner/...`
Commit: `fix(security): remove admin fallback, scrub example templates, and harden hetzner client`

---

### Task 4: Recurring Background Billing Daemon & Runway Monitoring

**Files:**
- Create: `clients/manova/shop-tgbot/internal/domain/billing/worker.go`
- Test: `clients/manova/shop-tgbot/internal/domain/billing/worker_test.go`
- Modify: `clients/manova/shop-tgbot/cmd/shopbot/main.go`

**Interfaces:**
- Produces: `BillingWorker` executing minute-by-minute accruals across active servers with automated shutdown when runway reaches zero.

- [ ] **Step 1: Write failing test for minute billing worker**
Simulate active server running past minute 60, verify worker calculates charge, debits wallet via `DebitAtomic`, writes `minute_postings`, and flags depleted accounts.

- [ ] **Step 2: Implement BillingWorker**
- Every 60s ticker: queries `s.store.Billing().ListActiveResources(ctx)`.
- Calculates minute delta since `posted_through_minute`.
- Debits user wallet using `s.store.Wallets().DebitAtomic(...)` with `kind: wallet.KindUsage`.
- If wallet drops below floor: schedules grace period and notifies user.
- If grace period expires: triggers server shutdown via provider.

- [ ] **Step 3: Wire BillingWorker in cmd/shopbot/main.go**
Start worker goroutine in background with context cancellation on SIGTERM.

- [ ] **Step 4: Run tests and commit**
Run: `go test -C clients/manova/shop-tgbot -race -v ./internal/domain/billing/...`
Commit: `feat(billing): implement recurring minute billing worker and runway protection`

---

### Task 5: Web One-Time Password Reveal & Credential Envelope Encryption

**Files:**
- Create: `clients/manova/shop-tgbot/internal/adapters/telegram/reveal.go`
- Modify: `clients/manova/shop-tgbot/internal/adapters/telegram/buy_flow.go`
- Modify: `clients/manova/shop-tgbot/internal/adapters/telegram/servers_mgmt.go`
- Modify: `clients/manova/shop-tgbot/internal/platform/crypto/envelope.go`
- Test: `clients/manova/shop-tgbot/internal/adapters/telegram/reveal_test.go`

**Interfaces:**
- Invariant: Cleartext root passwords and rescue credentials are NEVER stored unencrypted and NEVER displayed in chat history. Users access credentials via single-use encrypted reveal links with 15-minute TTL.

- [ ] **Step 1: Implement AES-256-GCM envelope encryption**
Encrypt credentials at rest using `CREDENTIAL_KEK`.

- [ ] **Step 2: Implement one-time token reveal link generator**
Generate HMAC-signed, time-limited single-use reveal token.

- [ ] **Step 3: Redact passwords from Telegram messages**
Replace inline passwords in `buy_flow.go` and `servers_mgmt.go` with one-time reveal links (`REVEAL_BASE_URL/reveal?token=...`).

- [ ] **Step 4: Run tests and commit**
Run: `go test -C clients/manova/shop-tgbot -race -v ./internal/adapters/telegram/...`
Commit: `feat(security): implement one-time password reveal and envelope encryption`

---

### Task 6: RFC 6238 TOTP Verifier & Privileged Admin Action Gate

**Files:**
- Create: `clients/manova/shop-tgbot/internal/adapters/totp/verifier.go`
- Test: `clients/manova/shop-tgbot/internal/adapters/totp/verifier_test.go`
- Modify: `clients/manova/shop-tgbot/internal/adapters/telegram/bot.go`

**Interfaces:**
- Implements: `port.TotpVerifier`.
- Invariant: Sensitive admin operations (manual balance adjustments, role elevation, blocking users) require valid TOTP verification code.

- [ ] **Step 1: Implement TotpVerifier using RFC 6238**
Implement `VerifyCode(ctx context.Context, adminID int64, code string) (bool, error)`.

- [ ] **Step 2: Gate admin mutations with TOTP challenge**
In `handleAdminWalletAdjustExec` and `handleAdminUserSetRole`: challenge admin for TOTP code or check active verified session.

- [ ] **Step 3: Run tests and commit**
Run: `go test -C clients/manova/shop-tgbot -race -v ./internal/adapters/totp/... ./internal/adapters/telegram/...`
Commit: `feat(security): implement TOTP verifier and gate privileged admin actions`

---

### Task 7: Full Verification, GitHub PR & Production Deployment

**Files:**
- All touched files

- [ ] **Step 1: Run comprehensive test suite**
Run: `go test -C clients/manova/shop-tgbot -race -count=1 ./...`

- [ ] **Step 2: Create GitHub Pull Request and Squash-Merge**
Push `feat/security-hardening` to GitHub.
Create PR via `gh pr create`.
Squash-merge PR into `main`.

- [ ] **Step 3: Production Deployment**
Build production binary: `go build -trimpath -ldflags="-s -w" -o /tmp/shopbot-prod ./cmd/shopbot`.
Stop service, copy binary to `/srv/shop-tgbot/current/shopbot`, start service.
Verify logs: `sudo journalctl -u manova-shop-tgbot.service -n 20 --no-pager`.
