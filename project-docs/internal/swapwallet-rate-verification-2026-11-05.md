---
cursor:
  subagentId: "bc-f48c4059-c415-5acb-9aed-f7060db260b4"
---

# USD rate source: SwapWallet primary, Wallex fallback (round 11 verification)

Status: verified from the store environment on 2026-11-05 (UTC), round 11 of the owner decisions. Related: `docs/shop-system-design.md` sections 4.3, 26.3, and 27.

Ownership note: `docs/usd-rate-source.md` belongs to another subagent (frontmatter `bc-f4e25742-b315-5d5c-9ce7-037837aab7e7`). It was not edited in this round. Its status line still says the owner decision is open. The owner's SwapWallet choice is recorded here instead, and the owner of `usd-rate-source.md` should copy it there.

## 1. Decision (owner, round 11)

- **Primary:** SwapWallet, pair `USDT/IRT`, from its public market price endpoint.
- **Fallback:** Wallex, symbol `USDTTMN`.
- **Rejected:** the Telegram price channel `t.me/s/irancurrency`. It is a third-party channel with no published method and an hourly cadence. It is kept only as a rejected option.

## 2. SwapWallet: verified

| Fact | Evidence |
|---|---|
| `GET https://swapwallet.app/api/v1/market/prices?pair=USDT/IRT` returns HTTP 200 with `application/json` and no `Authorization` header | Probed 2026-11-05, four probes plus one header probe. A probe with `pair=USDT%2FIRT` also returned 200. No API key was used or requested |
| Response shape: `{"status":"OK","result":{"<BASE>/<QUOTE>":"<decimal string>", ...}}` | One response listed 373 pairs. Values are strings, for example `"USDT/IRT":"270850"` |
| No timestamp, bid, or ask fields in the response | Full response read. Each pair has one value |
| `USDT/IRT` is the Tether price in Toman-scale IRT | Probes returned 270890, 270850, and 270660, about 20 seconds apart. The value is live, not cached. Wallex's TMN value at the same time was about 270,490, so the scale is Toman. This scale comparison is an inference, not a documented fact |
| Pair naming is `TOKEN/QUOTE`. The reference currencies are only USDT and IRT | API reference, section "Live prices" (Persian): "نام هر جفت قیمتی به‌صورت TOKEN/QUOTE ... ارزهای مرجع تنها USDT و IRT هستند." So the Tether/Toman pair is `USDT/IRT`, not `USDTTMN` |
| The endpoint is listed as `get/v1/market/prices` under the Market operations | API reference, Market operations list |
| The docs describe keys as `Authorization: Bearer apikey-...`, and say keys skip the two-step checks | API reference, authentication section. The reference page does not say whether the market prices endpoint is public |

Other pairs in the same response (for example `ETH/IRT`, `BTC/IRT`) confirm that IRT is the quote currency for every pair.

## 3. SwapWallet: not verified

- **Buy or sell side.** The docs do not say whether `USDT/IRT` is the price at which a user buys USDT, the price at which a user sells USDT, or a mid price. The only example is `TON/USDT`, which is described as the TON price in USDT. The landing page (`https://swapwallet.app`) showed "Current Price Tether ≈ 268,133 Toman" next to a buy-with-Toman display, at another moment. That suggests the buy side but does not prove it. The side must be confirmed before launch (design section 26.3).
- **Terms of public access.** The reference groups the endpoint with keyed endpoints. Unauthenticated access works today, but the terms do not promise it, and it can change without notice. If it requires a key, that is a new owner decision. Nothing in this round requested or stored a key.
- **Update cadence and precision.** The value changed between probes, but the update interval and the number of decimal places are not documented.
- **Rate limits.** No rate-limit text was found in the reference text read.
- **Status page.** A search result reported high uptime for the Live Prices API and some incidents on the markets service in early November 2026. The status page was not fetched in this round, so this is unverified.

## 4. Wallex USDTTMN: verified (fallback)

- `GET https://api.wallex.ir/v1/markets` returns 200 JSON. `result.symbols.USDTTMN` has `quoteAsset: "TMN"`, `faQuoteAsset: "تومان"`, `minNotional: 50000`, and `stats` with string prices `bidPrice`, `askPrice`, `lastPrice`, plus `lastTradeSide`. At the probe, bid was 270479, ask 270497, last 270478.
- `GET https://api.wallex.ir/v1/depth?symbol=USDTTMN` returns 200 JSON. `result.ask` and `result.bid` are order-book lists. The best ask was 270499 and the best bid was 270488 at the probe.
- `GET https://api.wallex.ir/v1/trades?symbol=USDTTMN` returns 200 JSON. `result.latestTrades[]` has `symbol`, `quantity`, `price` (string), `sum`, `isBuyOrder` (boolean), and `timestamp` (RFC 3339, UTC, for example `2026-11-05T16:21:47Z`).
- **Unverified:** Wallex's terms and rate limits. Wallex's own API reference was not read. The meaning of `bidPrice` and `askPrice` is inferred from the names and the depth structure.

## 5. Comparison

At about 16:23 UTC, SwapWallet's `USDT/IRT` was 270850 and the Wallex mid (bid and ask average) was about 270,493. The gap is about 0.13%. This is a single-time comparison, not a validation of either source.

## 6. Effect on the design

- SwapWallet's value is the market rate `M` for the design's `market_irt_per_usd`, once the side is confirmed (section 3 above, design section 26.3). Until then, the manual rate stays available.
- The response has no timestamp. The shop stores its own fetch time as `observed_at`, and staleness is measured from it.
- Wallex is the fallback when SwapWallet is unreachable or returns no `USDT/IRT` value. The Wallex value is the best bid and ask midpoint, or the `stats` last price, once the mapping is confirmed.
- Parsing, jump, and range rules in `usd-rate-source.md` (sections 5 and 6) are not changed by this report.
