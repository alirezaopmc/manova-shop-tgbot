# Owner decisions, round 2 (2026-11-05)

Answers to the open questions from round 1 (see owner-decisions-2026-11-05.md). These override round 1 where they conflict.

1. Balance floor reached: the server is removed (deleted). The owner wrote "Remove". Read it as delete, and treat it as pending the owner's confirmation. Design a safeguard, such as warnings before the floor is reached.

2. C = the user's total purchase amount in the last 30 days, rolling, in Toman. Use the amount paid, ignoring fees and other adjustments. Example: total payments of 16M Toman give C = 16M Toman, which falls in the 1% fee bucket.

3. Segment thresholds (CUSTOMER above 1M Toman, LOYAL above 10M Toman) use the same 30-day rolling window as C.

4. The OTP for the admin's custom negative-limit change is TOTP, read from an authenticator app such as Google Authenticator. It is not sent through Telegram. Design enrollment, encrypted secret storage, and recovery codes.

5. Usage polling: accept the approach of recording start and stop timestamps and polling only for status changes. Hard requirement: billing must never miss a minute. Design gapless per-minute accounting from server start to stop, with idempotent per-minute ledger entries, detection and backfill of scheduler gaps, and reconciliation against provider data. A minute must never be lost or charged twice.

6. Pricing currency: catalog base prices are in USD. Use a reliable USD-to-Toman rate source. The sell rate is the market rate plus 5,000 Toman per USD. Show the user the base USD price and the converted rial amount. Fixed-price orders are re-quoted when the rate moves by more than 5,000 Toman from the rate at order creation.

7. Rate source: the owner proposed the Telegram channel post https://t.me/irancurrency/39563. It is edited in place and gets new posts every few hours. Parse it precisely. Handle edits, new posts, and missed updates, so no rate change is missed. Verify the source before relying on it.

8. Telegram Stars: dropped. Card payments and wallet top-up only.
