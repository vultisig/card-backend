# Architecture

Companion to the package docs (`internal/*/doc.go`). Describes the responsibility split between this service, the Vultisig apps, and the issuer (Reap modular API, `Reap-Version: 2025-02-14`), and the invariants the implementation must hold.

## Program configuration (fixed at issuer project creation)

- Program mode: **Consumer** · Account ownership: **User** · Funding model: **USER_FUNDED**.
- USER_FUNDED means: cardholders deposit their own stablecoins to issuer-provisioned per-account wallets; **the issuer always authorizes** transactions against that deposited balance. This service is never in the authorization hot path — its control is exercised ahead of time via spend policies.

## System shape

```
 iOS / Android / Desktop apps            card-backend                      Reap
 ───────────────────────────            ───────────────────────           ─────────────────────────
 MPC vault (signs everything    HTTPS   api: app-facing /v1        HTTPS  users / accounts / cards
 that moves funds or binds  ──────────► device: enroll, step-up ────────► policies / activities
 identity)                              ledger, policy, rewards,   push   webhooks (12 events,
                             ◄──────── autopilot, kyc, recon     ◄────── HMAC-signed)
                               push     webhook sink + fan-out
                                        VultiServer (existing Go service) for Fast-Vault co-signing
```

Devices are stateless readers keyed by `vault_pubkey` (stable across devices and re-imports). The backend is the source of truth for everything Vultisig-owned; the issuer is the source of truth for money facts (balance, authorizations, transaction lifecycle).

## Flows (owner per step)

1. **Onboard & KYC** — backend creates the issuer user (terms version + IP), advances the KYC application (managed provider SDK token relayed to the app, or share-token import for already-verified users); issuer decides; backend runs the status machine off the application-status webhook. No account or card exists until APPROVED.
2. **Vault binding** — app MPC-signs the issuer's auth message with both vault keys (EVM secp256k1 + SVM ed25519); backend creates the account with those signers and caches the per-chain deposit addresses. *Signers are the cardholder's future withdrawal co-signing keys — see Invariants.*
3. **Funding** — app performs a normal vault send (MPC-signed) to the account's own deposit address; backend routes chain/asset choice and tracks the deposit webhooks (only APPROVED deposits are spendable). Production chains: Base, Polygon, Solana.
4. **Spend** — issuer authorizes (balance + policies + platform limits). Backend consumes the two transaction webhooks into the ledger (event-keyed, idempotent, out-of-order safe; partial clearings, over-clearings, and standalone refunds are normal, not errors).
5. **Rewards & tiers** — backend computes tier from VULT **holdings** (on-chain balances of the vault's own addresses — no staking or locking) and monthly spend, compiles it into issuer spend policies, accrues cashback on cleared net amounts, and manages the VULT vesting/claim ledger. The issuer has no rewards primitives.
6. **Top-up & Autopilot** — a top-up is an ordinary vault send through the standard keysign flow, destination pre-filled with the card's own deposit address. The backend owns the low-balance trigger (it alone sees balance events) and notifies; automated top-ups arrive later via the plugin system, which executes within a user-granted policy — the backend still supplies only vault-derived destinations.
7. **Security surfaces** — 3DS challenge relayed as an in-app approval (respond within the issuer window); wallet-tokenization OTP relayed by push; PAN reveal brokered as a single-use short-TTL issuer URL rendered in an app WebView.
8. **Withdraw (planned issuer capability)** — designed as 2-of-2: issuer signature + cardholder signer signature, computed on balance net of holds. Backend will orchestrate initiate → co-sign (vault MPC ceremony) → track; it holds no signing power.

## Invariants (must hold in any implementation)

1. **No account creation without verified signers.** The registered signers are the user's future withdrawal rights. Creating an account with missing/unverified signers permanently degrades the cardholder's custody position.
2. **Destination-locking.** Refills and (future) withdrawals only ever target addresses derived from the bound vault. No API path may accept a free-form destination.
3. **No PAN/CVV/expiry anywhere.** Not in the database, not in logs, not proxied. Reveal URLs are passed through to the client and never fetched server-side.
4. **Webhook ingestion is idempotent and order-independent.** Dedupe on event id; signature verification with constant-time comparison and timestamp tolerance; consumers must tolerate replays and gaps.
5. **The backend never computes spendable balance.** It caches issuer balance facts and displays them; authorization headroom is issuer-owned.
6. **Rewards accrue only on cleared, net amounts** — reversals and refunds (including standalone refunds with no parent transaction) claw back accrual.
7. **Money-affecting actions require the vault, not the backend.** The backend can initiate and orchestrate, but every fund movement is signed by the user's MPC vault (or, later, executed by the plugin system within a policy the user explicitly granted).
8. **Boundary changes require step-up.** New device, card re-bind, autopilot source change: fresh MPC threshold signature, never just a bearer token.

## Data model (sketch — schema lands with the first implementation PR)

`vaults` (vault_pubkey PK, issuer ids, kyc status) · `devices` (device_pubkey, token, status) · `cards` · `txn_cache` (event-sourced from webhooks) · `policies_compiled` (tier → issuer policy ids) · `rewards_ledger` (accruals, vests, claims) · `autopilot_policies`.

## Non-goals

- Custody of user funds or key material (vault shares never touch this service).
- Re-implementing issuer logic (authorization, FX, KYC decisions).
- A public API — the only consumers are the Vultisig apps and issuer webhooks.
