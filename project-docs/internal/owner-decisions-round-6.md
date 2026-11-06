# Owner decisions, round 6 (2026-11-05)

These override earlier rounds where they conflict.

1. Sanctions and legality: the owner states it is legal for customers in Iran to use Hetzner servers through the shop, and that the Google resale model is allowed. Record this as the owner's determination. Keep a pre-launch check that the provider terms match this statement, since the owner's view is not a legal opinion.
2. Refunds: confirmed. Refund the unused base, keep the fee, and apply the 10% commission to all refunds.
3. Floor breach: confirmed. Deletion is automatic after the grace period.
4. Running-server price: follow the sell rate. The user must be notified each time the price of a running server changes because of the rate. Notification text must be in Persian and explain the change.
5. Custom negative floor: allowed. Lowest allowed custom floor is -1,000,000 Toman.
6. Phone verification: required before the first top-up. The phone number must be an Iranian mobile number (+98).

## New requirements

7. Multi-currency wallet: each wallet holds a Toman balance and a USD balance.
8. USD top-up by crypto: users send screenshots of crypto payments. The owner reviews each one and credits the USD balance manually, using the same review and anti-fraud rules as receipts. The owner may later automate this with the SwapWallet API. Design the top-up port so this can be swapped in later without changing the flow.
9. Buying with a wallet: at purchase, the user can pay from the wallet balance. Users can also create a one-month checkout. This pays for one month of the chosen server, and the amount credits the wallet first. The purchase then uses the wallet.
10. Informative texts at purchase: tell the user that paying a month in advance increases their wallet balance, and that the wallet can be used for any other resource.

## Readings to flag for confirmation

- Custom floor -1,000,000 Toman is read as the lowest allowed floor.
- Wallet floors apply to the combined value of both balances, converted to Toman at the current sell rate. The owner has not said this yet, so mark it as a proposed default and list it as an open question.
