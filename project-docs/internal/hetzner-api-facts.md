---
cursor:
  subagentId: "bc-2f347e95-1ead-5b53-9ba9-cdff47eca1f6"
---

# Hetzner Cloud API facts for shop_manovabot

Research only. No API token was used, no servers were created, no account settings were changed, and no code was pushed.

Checked on 2026-11-05 (UTC).

## Sources

| Ref | URL | What it is |
|---|---|---|
| S1 | https://docs.hetzner.cloud/cloud.spec.json | Official Cloud API OpenAPI 3.1 document (version 1.0.0) that powers docs.hetzner.cloud. The rendered reference pages are client-side only and could not be read directly, so quotes below come from this document. Its `info.description` holds the Overview, Pagination, and Rate Limiting sections. Anchors used by the docs site: `#description/pagination`, `#description/rate-limiting`. |
| S2 | https://docs.hetzner.cloud/changelog | Official API changelog. Entries carry anchor IDs with dates, for example `2026-08-31-debian-11-image-is-deprecated`. |
| S3 | https://docs.hetzner.com/cloud/billing/faq/ | Official Cloud billing FAQ. Page header says "Last change on 2020-07-07". |
| S4 | https://www.hetzner.com/cloud/ and https://www.hetzner.com/pricing/ | Official pricing pages. Show monthly prices excl. VAT in EUR or USD. They do not state the hourly billing rule. |

Third-party pages (a Hetscale docs page, a GitHub SDK doc, and a GitHub issue) were seen in search results. They are not official and are not used to mark anything VERIFIED. Where they mention something the official docs do not confirm, the item is marked UNVERIFIED.

Not done: no live `GET /images` call, because it needs a project token and this research forbids account use. The live image catalog is therefore UNVERIFIED (see section 8).

---

## 1. Rate limit and rate-limit headers

**Status: VERIFIED** (limit and headers). **UNVERIFIED** (`Retry-After` on 429, and any separate pool for action endpoints).

- Source: S1, `#description/rate-limiting`.
- Quote: "The default limit is 3600 requests per hour and per Project. The number of remaining requests increases gradually. For example, when your limit is 3600 requests per hour, the number of remaining requests will increase by 1 every second."
- Headers, quoted from S1:
  - `RateLimit-Limit`: "contains the total number of requests you can perform per hour."
  - `RateLimit-Remaining`: "contains the number of requests remaining in the current rate limit time frame."
  - `RateLimit-Reset`: "contains a UNIX timestamp of the point in time when your rate limit will have recovered, and you will have the full number of requests available again."
- Burst: "Burst requests are allowed."
- Exceeded: HTTP `429`, error code `rate_limit_exceeded`, described as "Error when sending too many requests." (S1 error table).
- Scope: the limit applies to all requests, authenticated or not, and is per Project, not per token.
- `Retry-After`: UNVERIFIED. It does not appear in S1 or S2. Third-party SDK docs mention it, but that is not an official source.
- Separate pool for action endpoints (for example `shutdown` or `add_target`): UNVERIFIED. Only a Hetscale docs page claims this ("Requests to action endpoints ... count in a separate pool of twice that, Hetzner support told us in September 2026"). Not in any official source. Do not rely on it.
- Design note: the design's 600 read requests per hour cap (section 8.7) is well under 3600, but it still has to cover provisioning and deletion. Also, S1 says the value is "the default", so the design should read the headers at runtime, not hardcode 3600.

## 2. Pagination (page size and mechanism)

**Status: VERIFIED.**

- Source: S1, `#description/pagination`, and the `per_page` schema on every list operation.
- Quote: "A `page` parameter specifies the page to fetch. The number of the first page is 1. For invalid values, page 1 is fetched."
- Quote: "A `per_page` parameter specifies the number of items returned per page. The default value is 25 and the maximum is 50, unless otherwise specified in the documentation. For invalid values, the default is used."
- Check: all 39 `per_page` parameters in S1 have `default: 25` and `maximum: 50`. `GET /servers` and `GET /images` are both included. No list operation overrides this.
- Body metadata: "if the response body is JSON and the root object is an object, that object has a `pagination` object inside the `meta` object". Keys: `page`, `per_page`, `previous_page`, `next_page`, `last_page`, `total_entries`. These may be `null`.
- `Link` header: deprecated. S1 says "This header is deprecated and will be removed in the future". S2 gives the date: "will be removed on 1 April 2027". Use `meta.pagination` instead.
- Design note: `GET /v1/servers?label_selector=shop_managed=true&per_page=50` is valid. The "(verify page size)" item in design section 8.7 is now settled: max 50. Use `meta.pagination.next_page`, not the `Link` header.

## 3. Billing of powered-off servers, and the billing rules

**Status: VERIFIED** from S3, with one caveat. The FAQ's last change date is 2020-07-07. No newer official billing text was found, so it is the current official statement but could be out of date. Re-check before launch.

- Powered-off servers are billed. Quote (S3, "Do you bill servers that are off?"): "Yes, you pay for a server that has completed the creation process for as long as it exists, regardless of whether it is turned on or not." Also: "We will bill you for your servers until you delete them, independent of their state."
- Billing model. Quote (S3): "Servers have both a monthly price cap and a price per hour. Your server's bill will never exceed its monthly price cap."
- Cap logic. Quote (S3): "Mathematically speaking, we will bill you for the minimum amount, whether that is the monthly price cap OR the hourly price multiplied by the number of hours you used the server."
- Partial hours. Quote (S3): "We always round up the hourly usage of a server. If you create a server just for a few minutes, we will still bill you for one whole hour."
- Early deletion. Quote (S3): "If you delete your cloud server before the end of the billing month, we will only bill you for the hourly rate."
- Idle servers. Quote (S3): "Even if you aren't actively using your server, we will bill you for it."
- Invoice cadence. Quote (S3): "After the first invoice, we will create invoices based on full calendar months."
- Usage visibility. Quote (S3): "It is not possible to view the usage data if you have not yet received your invoice for the previous month." and "In the Hetzner Console, you can only view the usage data for the current month."
- Primary IPs billed separately. Quote (S3): "We bill Primary IPs (IPv4, IPv6) and cloud servers separately ... We will bill you for all Primary IPs that have finished the creation process, including Primary IPs that are not assigned to any cloud server."
- Primary IPv4 monthly price: https://docs.hetzner.com/cloud/servers/overview/ (last change 2022-06-28) says "€ 0.50/month (excl. VAT)". UNVERIFIED for the current price. Use `GET /pricing` (section 7).
- Traffic: only outgoing traffic is billed, in blocks of 100 MB beyond the included amount (S3). Not in the original question but it is a billed item.
- Interpretation for the design:
  - Billing is per started hour, rounded up. It is not per minute.
  - The monthly price is a cap, not a flat charge. The text does not state an exact formula beyond "minimum amount".
  - Design sections 8.1 and 8.2 (minute-level postings, reversal of unused precharge on stop) do not match this model. Hetzner charges a whole started hour, so a shop that refunds unused minutes would be paying more than it bills. This needs an owner decision.

## 4. Rescue system and root password reset: endpoints and return values

**Status: VERIFIED.**

| Purpose | Method and path | operationId | Returns action resource? | Notes (quoted from S1) |
|---|---|---|---|---|
| Enable rescue | `POST /servers/{id}/actions/enable_rescue` | `enable_server_rescue` | Yes. `ActionResponse` with `action` and `root_password` | "Enable the Hetzner Rescue System for this Server." Request body `type` is `linux64` (default). Optional `ssh_keys`. "Enabling rescue mode will not reboot your Server — you will have to do this yourself." |
| Disable rescue | `POST /servers/{id}/actions/disable_rescue` | `disable_server_rescue` | Yes. `ActionResponse` | "Rescue Mode is automatically disabled when you first boot into it or if you do not use it for 60 minutes." |
| Reset root password | `POST /servers/{id}/actions/reset_password` | `reset_server_password` | Yes. `root_password` and `action` | "Resets the root password. Only works for Linux systems that are running the qemu guest agent. Server must be powered on (status `running`) in order for this operation to succeed." |

- Reset fallback. Quote: "If this does not succeed you can use the rescue system to netboot the Server and manually change your Server password by hand."
- Design note: the design's "password reset action (verify the name)" is `reset_password` (operationId `reset_server_password`). A reset requires a running server with the guest agent. If the server is off, the design's re-issue path has to use rescue, and the rescue step needs a reboot.
- Design note: the `root_password` returned by `enable_rescue` is for the booted rescue system, not for the server's own disk (quote: "can be used to access the booted rescue system").

## 5. Is the root password returned only at creation?

**Status: the claim "only at creation" is NOT supported. Corrected below.** The "returned once" part is UNVERIFIED.

The root password is returned in four places, all quoted from S1:

1. `POST /servers` (`create_server`): "If you do not specify any `ssh_keys` we will generate a root password for you and return it in the response." Response field `root_password`: "Root password when no SSH keys have been specified."
2. `POST /servers/{id}/actions/rebuild` (`rebuild_server`): response field `root_password`: "New root password when not using SSH keys."
3. `POST /servers/{id}/actions/enable_rescue`: root password for the rescue system (see section 4).
4. `POST /servers/{id}/actions/reset_password`: "This will generate a new password for this Server and return it." Field: "Password that will be set for this Server once the Action succeeds."

- Not in the server object: `GET /servers/{id}` returns the server schema with no password field (verified property list).
- "Returned once": S1 does not say the password can never be read again. UNVERIFIED. The design should not depend on either behaviour.
- Delivery of a reset password: it comes back in the POST response, but it only becomes active when the action succeeds. Do not reveal it until the action reaches `success`. Poll `GET /actions/{id}` (operationId `get_action`) or `GET /servers/{id}/actions/{action_id}`.
- Design note: section 10 says "Hetzner returns a generated root password once, in the create response." This is only true for creation without SSH keys. The reveal design still works, but the reset-password path has to wait for the action.

## 6. Server uptime metric, and created and started timestamps

**Status: VERIFIED** for the absence of an uptime metric and the lack of a `started` field. **UNVERIFIED** for how long action history is kept.

- Metrics endpoint: `GET /servers/{id}/metrics` (`get_server_metrics`). Quote (S1): "You must specify the type of metric to get: cpu, disk or network." The defined series are `cpu`, `disk.0.iops.*`, `disk.0.bandwidth.*`, `network.0.pps.*`, and `network.0.bandwidth.*`. None is an uptime or power-state series.
- Retention. Quote (S1): "Metrics are available for the last 30 days only."
- Server object fields with time or traffic: `created` (RFC 3339). No `started` field. Also `included_traffic`, `outgoing_traffic`, and `ingoing_traffic` for the current billing period. These are not uptime.
- Action objects carry `started` and `finished` (RFC 3339, `finished` may be null), plus `command`, `status` (`running`, `success`, `error`), and `resources`. Endpoints: `GET /servers/{id}/actions` (`list_server_actions`) and `GET /servers/actions` (`list_servers_actions`).
- Action history retention: S1 gives no retention period. UNVERIFIED. Do not depend on old action history being available.
- Reconciling billed time: the official FAQ says usage data is on the invoice, and that the console only shows the current month. There is no API endpoint that returns billed hours. Use the bot's own lifecycle records (`created`, action `started` and `finished`), and reconcile against invoices.
- Design note: section 8.1 says `started_at` is the purchase time and `stopped_at` is taken from action history. Store `created` from the server object and action timestamps from the create action. Do not expect a `started` field.

## 7. Price list endpoint, currency, and net and gross values

**Status: VERIFIED.**

- Endpoint: `GET /pricing` (operationId `get_pricing`, summary "Get all prices"). Quote (S1): "Returns prices for all resources available on the platform. VAT and currency of the Project owner are used for calculations. Both net and gross prices are included in the response."
- Currency: the response has `pricing.currency`, described as "Currency the returned prices are expressed in, coded according to ISO 4217" (example EUR). It is not an API parameter. It follows the project owner's account currency.
- Account currency is fixed. Quote (S3): "It is not possible to change the currency after an account has been created." Also: "A project is billed in the currency that the owner of the project has configured for their account."
- So the answer is EUR or USD depending on the account, not both. The design must read `pricing.currency` and must not assume USD.
- Structure: `pricing.server_types[]` with `id`, `name`, and `prices[]`. Each `prices[]` item has `location`, `price_hourly` `{net, gross}`, `price_monthly` `{net, gross}`, `included_traffic` (bytes), and `price_per_tb_traffic` `{net, gross}`. Values are decimal strings.
- Other sections: `pricing.vat_rate` (decimal string), `primary_ips`, `floating_ips`, `image` (`price_per_gb_month` `{net, gross}`), `volume`, `server_backup`, `load_balancer_types`, `floating_ip`.
- The public pricing page (S4) shows monthly prices excl. VAT, for example "€ 5.99 per month, excl. VAT". Use it only as a cross-check.
- Design note: the shop's USD catalog needs a conversion from `pricing.currency`. The design's `price_usd_micro` assumption and the hourly-rate source (`/pricing` `price_hourly`) must be reconciled with the account currency. Decide whether to use `net` or `gross`. Hetzner's invoice is a net-plus-VAT amount, and the VAT rate depends on the owner's country.

## 8. Ubuntu and Debian images for Cloud servers

**Status: MIXED.** The API structure and the lifecycle events are VERIFIED from S1 and S2. The current full catalog is **UNVERIFIED**, because no official static list exists and the live endpoint needs a token that was not used.

### 8.1 What the official docs say about image names and IDs

- Image name is the stable key for system images. Quote (S1, `Image.name`): "Unique identifier of the Image. This value is only set for system Images." Example: `ubuntu-24.04`.
- Names are unique per architecture, not globally. Quote (S2, entry `2023-04-12-new-api-fields-to-support-arm64-architecture`): "Previously, the image name uniquely identified the image. This is no longer true. Instead, the image is now uniquely identified through the combination of name & architecture. This means that we now have (as an example) two images named ubuntu-22.04, one with architecture x86 and one arm."
- Creation by name is supported. Quote (S2): "If you create servers from images (or snapshots) by passing the ID, you need to take care that you pass an image with a compatible architecture. If you create servers by passing the image name, our API will select the correct image for you." Also S1 for `create_server`: "For `image` you can either use the ID as listed in `/images` or its name."
- Architecture filter. `GET /images` accepts `architecture` (`x86` or `arm`) and `name`, and `type=system` can be used to limit to system images. Quoted from S1 parameter list.
- Lifecycle fields (S1, `Image.deprecation`): `announced` and `unavailable_after`. Quote: "Once this date is reached, the resource will not be returned by list endpoint, and the resource can not be used to create new resources. For example, if this is an image, you can not create new servers with this image after the mentioned date."
- Field removed. S2 (entry dated 2026-11-02, anchor `2026-11-02-removed-the-deprecated-field`): "The response field deprecated of all endpoints that return 'Images' ... was removed ... Please use the deprecation field instead." Do not read the old `deprecated` field.
- `os_flavor` enum (S1) includes `ubuntu` and `debian`. `os_version` is a string or null.
- `rapid_deploy` (S1, boolean) marks Rapid Deploy images.

### 8.2 Ubuntu and Debian entries from the changelog

Versions and names are from the changelog entry text. Status is from the changelog. Where the changelog does not say, the status is marked.

| Name | Distribution and version | Deprecated? | New servers allowed? | Name stable? | IDs (x86 / arm) | Changelog anchor and quote (S2) | Status |
|---|---|---|---|---|---|---|---|
| `ubuntu-26.04` | Ubuntu 26.04 | No deprecation found | Changelog says available | Name, with arch | 387894169 / 387894171 | `2026-05-18-fedora-44-and-ubuntu-2604-now-available`: "The Ubuntu 26.04 image can be used as ubuntu-26.04 (IDs 387894169 for x86 and 387894171 for arm)." | Added 2026-05-18. Rapid Deploy. Live `deprecation` field UNVERIFIED. |
| `ubuntu-24.04` | Ubuntu 24.04 | No deprecation found | Changelog says available | Name, with arch | 161547269 / 161547270 | `2024-04-26-fedora-40-and-ubuntu-2404-are-available`: "and ubuntu-24.04 (IDs 161547269 (x86) & 161547270 (arm)) have been added." | Added 2024-04-26. Live `deprecation` field UNVERIFIED. |
| `ubuntu-22.04` | Ubuntu 22.04 | No deprecation found | UNVERIFIED | Name, with arch | UNVERIFIED (not stated in S2) | `2026-05-18-...` mentions Ubuntu 22.04 for cloud-init private network auto-configuration. Not a deprecation or availability statement. | UNVERIFIED for current availability. |
| `ubuntu-20.04` | Ubuntu 20.04 | Yes. Deprecated 2025-04-30 (anchor `2025-04-30-ubuntu-2004-deprecated`). | No. Unavailable for new orders since 2025-08-01 (anchor `2025-08-01-ubuntu-2004-is-unavailable-for-new-orders`): "The image ubuntu-20.04 (IDs 15512617 (x86) & 103908070 (arm)) is no longer available when creating new servers." | Name, with arch | 15512617 / 103908070 | As quoted | Do not use. |
| `ubuntu-18.04` | Ubuntu 18.04 | Yes. Deprecated 2023-05-31. | No. Unavailable for new orders since 2023-08-31. | Name, with arch | 168855 / 103908017 | `2023-05-31-ubuntu-1804-deprecated`: "...will no longer be available when creating new servers after 31 August 2023." | Do not use. |
| `debian-12` | Debian 12 | UNVERIFIED | UNVERIFIED | UNVERIFIED | UNVERIFIED | No changelog entry found for debian-12. | UNVERIFIED. |
| `debian-13` | Debian 13 | UNVERIFIED | UNVERIFIED | UNVERIFIED | UNVERIFIED | No changelog entry found for debian-13. | UNVERIFIED. |
| `debian-11` | Debian 11 | **Yes.** Deprecated 2026-08-31 (anchor `2026-08-31-debian-11-image-is-deprecated`). | **Only until 2026-11-30.** Quote: "The image debian-11 (IDs 45557056 (x86) & 103907373 (arm)) is deprecated and will no longer be available when creating new servers after 30 November 2026." | Name, with arch | 45557056 / 103907373 | As quoted | Still creatable as of 2026-11-05. Remove from the catalog before 2026-12-01. |
| `debian-10` | Debian 10 | Yes. Deprecated 2024-03-31 (anchor `2024-03-31-debian-10-image-is-deprecated`). | No. Unavailable for new orders since 2024-06-30 (anchor `2024-06-30-debian-10-image-is-unavailable-for-new-orders`). | Name, with arch | 5924233 / 103905968 | As quoted | Do not use. |

Notes:
- Names follow the `<distro>-<version>` pattern in every changelog entry. The changelog does not announce any rename.
- Removed images keep their name in the changelog but stop being orderable. Treat names as stable identifiers, but expect the catalog to change.
- Only the changelog statements are VERIFIED. Anything not listed in the changelog is UNVERIFIED, including the full current catalog.

### 8.3 What could not be confirmed

- The full current list of Ubuntu and Debian images offered right now: UNVERIFIED. The live `GET /images` (`list_images`) answer needs a project token, and this research did not use one.
- Debian 12 and Debian 13: UNVERIFIED. They are not in the changelog, so they are neither confirmed nor announced as deprecated.
- Ubuntu 22.04 availability: UNVERIFIED. It is not announced as deprecated, but no official source confirms it is orderable now.
- Whether `GET /images` returns deprecated images before their `unavailable_after` date when `include_deprecated` is omitted. S1 gives the parameter description "Include deprecated Images." but not the default behaviour. UNVERIFIED.
- Whether `deprecation` is set on any Ubuntu or Debian image right now. UNVERIFIED without a live call.

### 8.4 Design recommendations for images

- Do not hardcode the image list. Read the live list at runtime with `GET /images?type=system&architecture=x86` (and `arm` if the shop sells ARM types), and check each image's `deprecation` object.
- Use `debian-11` only before 2026-11-30. Hide it from the catalog after that date, and do not let a saved order use it.
- Use names in the create request, not IDs, so the API chooses the architecture-correct image.
- Do not rely on `debian-12` or `debian-13` until a live `GET /images` confirms them.

---

## Summary of what the design should not rely on

1. Minute-level billing or unused-minute reversal. Hetzner bills each started hour, rounded up, capped at the monthly price (section 3).
2. "Root password only at creation." It is also returned on rebuild (without SSH keys), rescue enable, and reset. A reset password is active only after its action succeeds (section 5).
3. A `started` timestamp on the server object, or any uptime metric. Use `created`, action `started` and `finished`, and your own records. Metrics keep 30 days only (section 6).
4. A separate action-endpoint rate pool, or a `Retry-After` header. Neither is in the official docs (section 1).
5. A hardcoded currency. Prices come in the account currency, EUR or USD (section 7).
6. The Debian 11 image after 2026-11-30, and any Debian 12 or 13 or Ubuntu 22.04 availability, until confirmed by a live catalog read (section 8).
