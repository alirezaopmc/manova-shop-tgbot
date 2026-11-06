# Owner decisions (2026-11-05)

Source: owner's answers in the Project chat. Treat these as requirements. Items marked OPEN have no decision yet: keep a sensible default in the design and list the item under open questions.

## Roles and access
- Only two roles: USER and ADMIN. The owner is the only ADMIN (Telegram ID 6246642516). Do not hardcode the ID; load it from configuration.
- Server access: support both SSH key and root password.
- Google AI Pro is fulfilled manually by the owner. The bot only sells it.
- Support contact shown to users: @manova_support.
- Language: Persian UI, with English professional terms where needed. Support i18n.

## Payments
- Method 1, direct purchase: the user picks an item and the bot shows a payment message with the card holder name and card number.
- Method 2, wallet top-up: the user uploads a receipt screenshot. The owner accepts or rejects it, and an accepted amount is credited to the user's wallet.
- The receipt is the only top-up channel for now. Users can only send rials.
- The card number is editable by the admin from the bot. Initial value: 1234123412341234. Store it in the database, not in code.
- Currency: wallet balances and fee thresholds are in Toman (IRT). Users pay in rials (1 IRT = 10 IRR).
- Catalog items can have prices in several currencies (for example USD). The rate source and rate-lock rules are OPEN.

## Fee ladder
- A fee applies to every purchase. The first purchase is free.
- Let C be the user's total purchases in the last 30 days (rolling window), in Toman.
  - C < 1,000,000 IRT: fee 10%
  - C < 10,000,000 IRT: fee 3%
  - otherwise: fee 1%
- OPEN: whether C includes the purchase being made, and whether the fee is added on top or deducted from the price.

## Catalog (server items)
Fields per item: name (for example CX23), type (CX Cost-Optimized or CPX Regular Performance; Storage Box later), CPU, RAM, SSD, price per hour, price per month. Maximum RAM is 16 GB for now.

## Refunds and tax
- Refund: 10% commission. The refund goes from the user's wallet to their card.
- Items are tax-free, but the small fee above applies.
- Future item types will be described by the owner later. Keep the catalog model extensible.

## Segments
- NEW: default for new users.
- CUSTOMER: more than 1,000,000 Toman.
- LOYAL: more than 10,000,000 Toman.
- OPEN: is the threshold based on lifetime spend or on the 30-day rolling window?

## Billing rules by segment
- NEW users:
  - At most 1 server at a time.
  - Resource cost is charged 1 hour in advance from the wallet.
  - Example: a server at $0.1/h, with a wallet worth $1, leaves $0.9 after the charge.
- CUSTOMER users:
  - Wallet floor: -100 Toman. The balance may not go below this.
  - Usage is polled every minute and stored in the database.
  - Charged per minute in advance, like NEW users but with a 1-minute window.
- LOYAL users:
  - Wallet floor: -500 Toman.
  - Same per-minute polling and precharge as CUSTOMER.
- The admin can set a custom negative limit for a user from the bot. This requires a one-time password (OTP). The owner wrote "POTP", most likely meaning OTP. Confirm.
- OPEN: what happens when the balance reaches the floor (stop the server, power off, or delete)?
- Abuse: the owner asked to close all abuse paths, or discuss them. The design must list abuse vectors and mitigations, for example multi-account farming for NEW limits, create/delete timing, refund abuse, and receipt fraud.

## Hosting
- The owner asked whether to host on Cloudflare or the singino server. Recommend the best option with reasons. Do not decide on the owner's behalf.

## Data retention
- The owner asked what "retention" means. Define it in the design: how long to keep orders, receipts, usage metrics, logs, and deleted-account data, and propose periods. Keep them if reasonable.
