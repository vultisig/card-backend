# card-backend

Backend service for the Vultisig Card: a Visa card issued through [Reap's modular API](https://docs.reap.global), funded by the user's own stablecoin deposits (USER_FUNDED), embedded in the Vultisig apps.

**Status: pre-implementation scaffold.** This repo currently contains structure and architecture docs only — no business logic. The point of the initial PR is to agree on the shape before building. Read [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md) first, then the ADRs in [`docs/decisions/`](docs/decisions/).

## What this service is (and is not)

The issuer (Reap) owns the card rail: the spendable balance, every authorization decision, FX conversion, deposit crediting, KYC verdicts, transaction lifecycle, and PCI scope (we never see PAN/CVV). This backend consumes those facts via webhooks and never re-implements them.

What this service owns:

| Concern | Summary |
|---|---|
| Identity & devices | Vaults are the customer key. Device enrollment is proven by an MPC threshold signature; devices are stateless readers with revocable tokens. |
| Vault ↔ issuer mapping | `vault_pubkey ↔ Reap user/account/card`, plus the transaction cache mirrored from webhooks. |
| Tier → policy compilation | Program tiers become issuer spend policies (limits, velocity, channel/merchant restrictions), updated on tier change. Nothing this service does sits in the swipe hot path. |
| Rewards | Cashback accrual on cleared spend, VULT conversion, vesting/claim ledger, pool accounting. |
| Autopilot | Balance watching + policy-bounded vault→card refills. |
| Relays | 3DS challenge approval, wallet-tokenization OTP, PAN-reveal URL brokering, push fan-out. |
| Monitors | Average ticket, decline rate, revenue reconciliation. |

## Layout

```
cmd/card-backend/     entry point (HTTP service)
internal/
  device/             device enrollment, tokens, step-up      (auth boundary)
  reapclient/         typed Reap API client                   (only pkg that talks to Reap)
  webhook/            webhook sink: verify, dedupe, dispatch
  ledger/             vault↔Reap mapping + txn cache          (read source of truth)
  policy/             tier → spend-policy compiler
  rewards/            accrual, VULT conversion, vesting, pool
  autopilot/          refill orchestration
  kyc/                user + KYC application orchestration
  push/               APNs/FCM fan-out
  recon/              monitors + reconciliation
  store/              PostgreSQL persistence
migrations/           SQL migrations (none yet)
docs/                 architecture + decision records
```

## Development

Go 1.26, stdlib only so far.

```
make build   # go build ./...
make test    # go test -race ./...
make lint    # golangci-lint run
make run     # starts the HTTP service (GET /healthz)
```

## Reviewing this scaffold

Things deliberately open for input:
- Module boundaries (`internal/*/doc.go` states each package's responsibility — challenge the seams).
- [ADR 0003](docs/decisions/0003-postgres-single-binary.md) (PROPOSED): PostgreSQL + single binary until a real queue exists.
- Anything missing from [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md) §Invariants.
