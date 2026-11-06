---
cursor:
  subagentId: "bc-f4e25742-b315-5d5c-9ce7-037837aab7e7"
---

# USD to Toman rate source: SwapWallet primary, Wallex fallback

Status: owner decision, round 11 (2026-11-05). Primary: SwapWallet `USDT/IRT`. Fallback: Wallex `USDTTMN`. The Telegram channel `t.me/s/irancurrency` is a rejected option (appendix A). This document supersedes the round-1 verdict that made the channel the primary source.

Related: `docs/shop-system-design.md` sections 4.2 (sell rate), 4.3 (rate source port), 4.4 (re-quoting), 26.3 (verification before launch), and 27 (changelog). Verified facts and unverified items: [swapwallet-rate-verification-2026-11-05.md](../internal/swapwallet-rate-verification-2026-11-05.md). Facts in this document come from that report unless marked otherwise. Anything listed in section 4 is open and is not a fact.

## 1. Decision and verdict

- **Primary:** SwapWallet public market price, pair `USDT/IRT`. `GET https://swapwallet.app/api/v1/market/prices?pair=USDT/IRT`, value at `result["USDT/IRT"]`.
- **Fallback:** Wallex, symbol `USDTTMN`, from `https://api.wallex.ir/v1/markets`, using `result.symbols.USDTTMN.stats`.
- **Rejected:** the Telegram channel `t.me/s/irancurrency`. It is a third party with no published method. Its USD posts arrive roughly hourly, and it was silent for about 15.6 hours overnight in the observation window. Its terms of use are a grey area. The evidence is kept in appendix A in case the owner reopens it.
- **Manual rate:** the `ADMIN` can still set the rate with an audited manual observation (design section 4.3). This is the last resort when both sources fail.

## 2. Primary source: SwapWallet

### 2.1 Verified facts

| Fact | Evidence |
|---|---|
| `GET https://swapwallet.app/api/v1/market/prices?pair=USDT/IRT` returns HTTP 200 with `application/json`, with no `Authorization` header | Probed 2026-11-05. The encoded form `pair=USDT%2FIRT` also returned 200 |
| Response shape: `{"status":"OK","result":{"<BASE>/<QUOTE>":"<decimal string>", ...}}` | One response listed 373 pairs |
| Values are JSON strings, for example `"USDT/IRT":"270850"` | Same response |
| There is one value per pair. There is no timestamp, bid, or ask field | Full response read |
| The value is live. Probes about 20 seconds apart returned 270890, 270850 and 270660 | Three probes |
| Pair naming is `TOKEN/QUOTE`. The reference currencies are only USDT and IRT, so the Tether/Toman pair is `USDT/IRT`, not `USDTTMN` | API reference, "Live prices" section |
| The endpoint is listed as `get/v1/market/prices` under Market operations | API reference |

### 2.2 Parsing rules

1. Request with a 5-second timeout. Accept only HTTP 200 with a JSON body whose `status` is `"OK"`. Anything else is a failed poll.
2. Read `result["USDT/IRT"]`. A missing key is a failed poll. It is never read as zero or as a previous value.
3. The value is a decimal string. Parse it as an exact decimal (`Decimal` or integer), never as a float. Accept only `^\d+(\.\d+)?$`.
4. Range check: accept only values in `[100,000, 2,000,000]` IRT per USD. The band is configuration. Values outside it are rejected, not converted.
5. Jump guard: if the value differs from the last accepted value by more than 3%, hold it as quarantined. Publish it only if the next poll returns the same value (or within 0.5% of it), or if an admin confirms it, with an audit record.
6. Store every valid poll as an observation with `observed_at` (the shop's fetch time, UTC), `source_ref = "swapwallet:USDT/IRT"`, the raw string, and `price_side = "unconfirmed"` (section 4, item 1). Store a new version whenever the value changes. Nothing is overwritten.

`observed_at` is the shop's fetch time. The response has no timestamp, so the shop's own clock is the only time source (design section 4.3).

## 3. Fallback source: Wallex USDTTMN

### 3.1 Verified facts

- `GET https://api.wallex.ir/v1/markets` returns 200 JSON. `result.symbols.USDTTMN` has `quoteAsset: "TMN"`, `faQuoteAsset: "تومان"`, `minNotional: 50000`, and `stats` with string prices `bidPrice`, `askPrice` and `lastPrice`, plus `lastTradeSide`.
- At the probe in the verification round: bid 270479, ask 270497, last 270478.
- `GET https://api.wallex.ir/v1/depth?symbol=USDTTMN` (best bid 270488, best ask 270499 at the probe) and `GET https://api.wallex.ir/v1/trades?symbol=USDTTMN` also return 200 JSON.

### 3.2 Mapping and failover

- **Candidate mapping (unconfirmed):** the midpoint of `stats.bidPrice` and `stats.askPrice`. The `lastPrice` option is also open. Section 4, item 6.
- The same range check and jump guard in section 2.2 apply to Wallex values. Their observations use `source_ref = "wallex:USDTTMN"`.
- **Failover to Wallex:** after 3 consecutive failed or rejected SwapWallet polls, switch to Wallex and alert the admin.
- **Return to SwapWallet:** after 2 consecutive valid SwapWallet polls.
- Failover applies to fallback only. It never switches on a price difference. Section 7 covers the cross-check.

## 4. Facts still open

These items are not verified. Do not treat them as facts in the code, the UI, or the quote text.

| # | Open item | Why it matters | What the design does until it is resolved |
|---|---|---|---|
| 1 | **Buy or sell side of `USDT/IRT`.** The docs do not say whether the value is the price at which a user buys USDT, sells USDT, or a mid price. The only documented example is `TON/USDT`. The landing page showed "Current Price Tether ≈ 268,133 Toman" beside a buy display, at another moment. That suggests the buy side but does not prove it. | The shop's market rate `M` (design 4.2) must match the side. A wrong side shifts every price by the spread. | Store the value with `price_side = "unconfirmed"`. Launch is blocked until the side is confirmed (design 26.3). Until then, the manual rate sets quotes. |
| 2 | **Public access and terms.** Unauthenticated access works today. The reference groups the endpoint with keyed endpoints, and the terms do not promise public access. | If access needs a key, that is a new owner decision. | Monitor HTTP 401 or 403. If access changes, switch to Wallex and ask the owner about a key. No key has been requested or stored. A key, if ever needed, goes in configuration, never in code. |
| 3 | **Update cadence and decimal precision.** The value changed between probes. The interval and the number of decimal places are not documented. | Decides the poll interval and the rounding of IRT prices. | Use the default poll interval (section 5.1). Measure cadence in the first week. |
| 4 | **Rate limits.** No rate-limit text was found in the reference read. | Limits the poll frequency. | Default poll interval 5 minutes, with backoff on 429 or 5xx. |
| 5 | **Status page.** A search result reported high uptime and some incidents on the markets service in early November 2026. The status page was not fetched. | Reliability assumptions. | Unverified. Check before launch. |
| 6 | **Toman scale of `USDT/IRT` and the Wallex mapping.** The scale is inferred from the Wallex value at the same time (about 270,490), not documented. Wallex `bid` and `ask` meanings are inferred from the names and the depth structure. Wallex's own API reference was not read. | Wrong scale or wrong mapping gives a wrong price. | Range check (section 2.2) catches scale errors of 10x. The mapping is confirmed before Wallex is used for quotes. |
| 7 | **Wallex terms and rate limits.** Not verified. | Fallback use needs permission and limits. | Check Wallex's terms before launch. |

## 5. Update rules

### 5.1 Polling

- Poll SwapWallet every 5 minutes with ±30 seconds of jitter. Config key: `RATE_POLL_INTERVAL_SECONDS`, default `300`. The default is a proposal until item 4 is resolved.
- Timeout 5 seconds. One retry after 10 seconds, and no more.
- Use an honest User-Agent that identifies the service. Do not impersonate a browser.
- Each poll writes one observation row, whether the value changed or not.
- Cache the current rate in memory and in the database. Quoting never fetches the source on the request path.

### 5.2 Change detection

- Keep `last_accepted` = `(source_ref, value, observed_at)`.
- Value differs from `last_accepted`: apply the validation in section 2.2, then publish a new `FxRate` version as `M`.
- Value unchanged: refresh `observed_at` on the current version (observations still record every poll).
- A failed poll leaves `last_accepted` unchanged and increments the failure counter.

### 5.3 Missed updates

- Each poll reads the current value. A missed poll delays detection by one interval. Nothing is lost, because the next valid poll returns the current value.
- A value that moves and moves back between two polls is not seen. A shorter interval reduces this risk, but the interval is limited by the unknown rate limits (item 4).
- Billing uses the rate in force for each minute (design 8.2). It uses the last valid rate, and a stale rate raises an alert (section 6).

## 6. Staleness and feed health

Quote staleness and feed health are different checks. Both are logged.

| Check | Rule | Effect |
|---|---|---|
| Rate age | `now - observed_at` of the last valid poll > `RATE_MAX_AGE_HOURS` (design default 6) | Refuse new quotes (design 4.3). Billing keeps the last valid rate and raises an alert (design 8.2) |
| Primary failure | 3 consecutive failed or rejected SwapWallet polls | Switch to Wallex (section 3.2) and alert the admin |
| Both sources failing | Both have no valid value | Alert. Quotes are refused once the rate age limit is reached. The admin may set an audited manual rate (design 4.3) |
| Frozen value | The same value for a long period during the day | Alert only. It does not mark the rate stale, because the cadence is unverified (item 3). The threshold is set after the first observation week |

The 6-hour limit is the design default. It is looser than a typical FX feed, so the owner may choose a shorter limit (section 10).

## 7. Cross-check

- On every poll where both sources return a valid value, compute `gap = |SwapWallet − Wallex midpoint| / SwapWallet`.
- If the gap is above 2% for 2 consecutive polls, raise an alert. Do not switch sources automatically.
- One sample at about 16:23 UTC showed SwapWallet 270850 against a Wallex midpoint of about 270,493, a gap of about 0.13%. That is one observation, not a validation.
- The cross-check is informational until the side in item 1 is confirmed. Bid and ask differ, so a comparison against the wrong side can show a false gap.

## 8. Other sources checked

Reachability from the research VM, 5 Nov 2026. These were checked during the channel research. None is the selected source. They are candidates for a second cross-check only.

| Source | URL | Result | Notes |
|---|---|---|---|
| tgju.org free-market USD page | `https://www.tgju.org/profile/price_dollar_rl` | HTTP 200. A figure of 261,681 appears | Label-to-value mapping not confirmed. `/profile/price_usd_sell` returned 404. Terms not checked |
| Bonbast | `https://bonbast.com` | HTTP 200. USD fields `usd1` and `usd2` are filled by JavaScript | The site labels values as "usd to rial", so Toman would be the value divided by 10. Not verified |
| Alanchand | `https://alanchand.com` | HTTP 301 to `https://alanchand.com/`, which returns 200 | Not parsed |
| Central Bank of Iran (CBI) | `https://www.cbi.ir/exchangerates/` | Blocked from the VM. A WAF "Request Rejected" page | Official reference, not a market price. May work from an Iranian IP |
| Nobitex | `https://api.nobitex.ir/...` | No HTTP response from the VM. `https://nobitex.ir` returns 200 | API not reachable from the VM |
| Ramzinex | `https://publicapi.ramzinex.com` | HTTP 403 from the VM | Not usable from the VM |

## 9. Terms of use

- **SwapWallet.** Unauthenticated access works today, but the terms do not promise it (item 2). Before launch, confirm the terms for market data in a commercial shop. If the terms do not allow it, ask the owner before using a key.
- **Wallex.** Terms and rate limits are unverified (item 7). Wallex's API reference was not read.
- **Pre-launch check.** Provider terms are on the launch checklist (design 24.1). Sanctions and resale risk are in design 23.3.
- **Telegram channel.** Not a source. Its terms are summarised in appendix A.7 for the record.

## 10. Owner decisions and verification

### 10.1 Owner decisions

1. Accept the design default `RATE_MAX_AGE_HOURS = 6` for refusing quotes, or choose a shorter limit.
2. Accept the default 5-minute poll interval until the cadence and the rate limits are known.
3. If SwapWallet requires a key or stops unauthenticated access: approve asking SwapWallet for access, or run Wallex only.

### 10.2 Verification before launch (design 26.3)

1. Buy or sell side of `USDT/IRT` (item 1). This is a launch gate.
2. Access and terms (items 2 and 7).
3. Cadence, decimal precision, rate limits, and the status page (items 3 to 5).
4. Toman scale and the Wallex mapping (item 6).

---

## Appendix A. Rejected option: Telegram channel `t.me/s/irancurrency`

This appendix records the 5 November research. It does not describe a live source. Reopen it only if the owner decides to, and only after the operator grants permission or a legal review allows the use.

### A.1 Why it is rejected

- Third party with no published method for its figures.
- USD posts roughly hourly, not "every few hours". The observed overnight gap was about 15.6 hours.
- Scraping `t.me/s` is at best a grey area under Telegram's terms (A.7).
- Format quirks: the unit is missing on some lines, and the in-text timestamp is about 73 minutes off the edit time.

### A.2 Reachability

| URL | Result |
|---|---|
| `https://t.me/s/irancurrency` | HTTP 200, 284,587 bytes. Last 20 messages (IDs 39544 to 39563) |
| `https://t.me/irancurrency/39563` | HTTP 200. Widget shell. `og:description` carries the post text, with no edit timestamp |
| `https://t.me/s/irancurrency/39563` | HTTP 200, 165,359 bytes. Single-post preview, 11 messages (39553 to 39563) |
| `https://t.me/irancurrency` | HTTP 200, profile page |
| `https://t.me/robots.txt` | HTTP 404 |

### A.3 Post structure (post 39563)

- Message ID: `data-post="irancurrency/39563"`.
- Series: the header line `نرخ فروش` (sell), with the change note `تغییر نسبت به دیروز`.
- USD line: `💵 دلار: 266,950 تومان  1140🔻 %0.43-`. The word `تومان` is missing on some lines (39544, 39549, 39557, 39558, 39559, 39560 and 39562).
- Text timestamp: `آخرین بروزرسانی: پنجشنبه ۱۴ آبان ۱۴۰۵ - ۱۷:۱۳` (Tehran time). It does not match the edit time, which was 16:00 Tehran.
- Edit marker: the meta text `edited` and `<time datetime="2026-11-05T12:30:26+00:00">`. The datetime is the last edit time.
- Buy rate: none. The string `خرید` does not occur in the 20-message preview.
- The `data-view` token carries the render time of the preview (13:45:05 UTC), not a post time.

### A.4 Recent USD posts and anomaly

| Message | Edit time (UTC) | USD sell (Toman) |
|---|---|---|
| 39544 | 4 Nov 08:23 | 269,080 |
| 39545 | 4 Nov 08:30 | 268,810 |
| 39546 | 4 Nov 09:32 | 268,970 |
| 39547 | 4 Nov 10:32 | 268,970 |
| 39548 | 4 Nov 11:32 | 269,000 |
| 39549 | 4 Nov 12:32 | 269,270 |
| 39550 | 4 Nov 13:30 | 269,090 |
| 39551 | 4 Nov 14:31 | 268,770 (last before the gap) |
| 39552, 39553 | 4 Nov 17:21, 17:22 | none (reposts of other rates) |
| 39554 | 5 Nov 06:07 | 267,710 (first after the gap) |
| 39555 | 5 Nov 06:30 | 267,710 |
| 39556 | 5 Nov 07:32 | 266,720 |
| 39557 | 5 Nov 07:37 | 267,120 |
| 39558 | 5 Nov 07:58 | 266,390 |
| 39559 | 5 Nov 08:30 | 266,740 |
| 39560 | 5 Nov 09:32 | 266,390 |
| 39561 | 5 Nov 10:31 | 267,070 |
| 39562 | 5 Nov 11:30 | 267,110 |
| 39563 | 5 Nov 12:30 | 266,950 |

- The largest step between consecutive USD values is 0.39%, across the overnight gap.
- **Anomaly in 39560.** Its USD line repeats the value and change figure of 39558, with the arrow reversed. This may be a copy error. It is a concrete example of a wrong-number risk.
- 18 of the 20 messages carry the `edited` marker. The preview shows only the final state of each message.
- Whether an edit keeps the same message ID is not verified. The Bot API's documented behaviour suggests it does, but one snapshot cannot prove it.

### A.5 Parser sketch, if reopened

- USD regex on the normalised text, anchored on the emoji and `دلار:`: `💵\uFE0F?\s*دلار:\s*(\d{1,3}(?:,\d{3})+|\d+)(?!\s*(?:کانادا|استرالیا))`. Do not match `💲 تتر`. Do not require `تومان`.
- Accept only the header `نرخ فروش`. Reject `نرخ خرید`.
- Range `[100,000, 2,000,000]`, jump guard 3%, and a rollback guard on message IDs (a lower ID is rejected).
- Key versions on `(message_id, edit_time, value)`. Source time is the `datetime` attribute, never the in-text timestamp.
- Staleness rules used in the research: 90 minutes during active hours, 180 minutes to stop quoting, and a 16-hour limit in the observed quiet window (21:00 to 10:00 Tehran). These were based on one overnight gap and would need a shadow run of at least 7 days.
- Poll every 5 minutes with a 20-message window. Do not publish until the shadow run passes.

### A.6 Failure modes

Wrong number, delayed update (about hourly), layout change, deleted post (rollback), unit change (rial instead of Toman), buy and sell mixed up, channel renamed or stopped, and fetch blocked or rate-limited. Each has a control in A.5.

### A.7 Terms and the official alternative

- Telegram's Terms of Service for Content Licensing prohibit access to user-generated content for purposes other than the ordinary use of the platform. The exception covers data needed to run a legitimate third-party Telegram client, bot or Mini App, strictly as required to operate that service.
- A shop fetching the public HTML preview is not a Telegram client. The exception does not clearly cover it.
- The posted content belongs to the publisher, whose own conditions also apply.
- The official route, if the operator agrees, is the Bot API. The operator adds the bot as an administrator, and the bot receives `channel_post` and `edited_channel_post`, with `message_id`, `date`, `edit_date` and text. Updates are kept for 24 hours at most, so the bot must poll or use a webhook continuously. The exact admin requirement should be checked with a test bot.
- An MTProto user client can read history, but the API and content-licensing terms restrict it, and it carries account-ban risk. Not recommended.
