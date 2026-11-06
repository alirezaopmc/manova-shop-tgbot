# Owner decisions, round 3 (2026-11-05)

These override earlier rounds where they conflict. Where the owner's reply is ambiguous, the design should state its reading and flag it for confirmation.

## Server actions
- Users can request Hetzner rescue mode and password reset for their own servers from the bot. Design the flow, its authorization checks, its rate limits, and how a new password is delivered (see the existing password-reveal rule).

## Hetzner API rate limits
- Global budget: 3,600 requests per hour, which is 60 per minute, shared by the whole system.
- Each user gets 30 requests per minute. The system serves at most 2 users concurrently.
- Enforce both the per-user limit and the global limit, with queueing and backoff. Requests that hit a limit must be delayed, never dropped, so no billing minute is lost.

## Floor breach and purchase disclosure
- Confirmed: the server is deleted when the wallet floor is breached, after the warnings and grace period in the design.
- Before any purchase, the user must see their segment, the wallet floor, what happens at the floor, and the billing policy. Show a short policy summary and a confirmation step.
- Include worked examples in the design for each segment (NEW, CUSTOMER, LOYAL), covering a normal run, a floor breach, and a power-off.

## Minimum billing block
- Confirmed: 60-minute minimum block.

## First-purchase promotion
- The first purchase is fee-free, up to 1,000,000 Toman.
- The first purchase has a hard limit of 1,000,000 Toman.
- If the wallet is topped up before the first purchase, the promotion is lost permanently.
- The owner's wording is brief. The design should state this reading and flag it for confirmation.

## Hosting
- The bot runs on the owner's singino machine.
- Design for that: run as a service with automatic restart, prefer long polling so no inbound port is needed, keep backups off the machine, and handle power or network outages without losing billing minutes. Downtime must be reconciled from provider timestamps, so no minute is lost.

## Power state and pricing
- Users can power off their servers in the control panel. Billing and the displayed rate change with the power state.
- Cache predictable values locally, such as price and usage estimates. Those can be computed from known start and stop times, and they shouldn't require a Hetzner call.

## Card holder name
- "Alireza Jafartash". Show it in payment messages. Store it as configuration, not in code.

## Confirmations
- Owner replied "Fine. Confirmed." to one item. The design must identify which item this is. If it can't tell, flag it for the owner.
