# card-backend

Vultisig Card backend service. An Echo HTTP server backed by Postgres that
authenticates vaults via a signed nonce and proxies user management to the
[REAP](https://docs.reap.global/) card-issuing API.

## Setup

```sh
make db-up               # starts local Postgres (card_backend db on localhost:5432, user/pass postgres)
JWT_SECRET=dev REAP_API_KEY=your-sandbox-key go run ./cmd/server
```

The server fails fast at startup if it can't reach Postgres, or if
`JWT_SECRET` or `REAP_API_KEY` is unset.

## Configuration

Env vars (all have a `config.json` equivalent, read from the working
directory if present; env always wins):

| Var | Default | Notes |
|---|---|---|
| `PORT` | `8080` | |
| `DATABASE_URL` | `postgres://postgres:postgres@localhost:5432/card_backend?sslmode=disable` | |
| `JWT_SECRET` | — | required |
| `REAP_API_KEY` | — | required |
| `REAP_ENV` | `sandbox` | `sandbox` or `prod` |

## API

- `GET /health` — pings the DB pool.
- `GET /nonce?public_key=<hex>` — returns the vault's next expected auth nonce.
- `POST /auth` — `{public_key, nonce, signature}`; verifies a secp256k1
  signature over the nonce and returns a JWT (`{access_token, token_type}`).

The routes below require `Authorization: Bearer <token>` from `/auth`, and
proxy to REAP for the vault's mapped REAP user:

- `POST /user` — `{email, phoneNumber, firstName?, lastName?, termsAcceptanceVersion}`;
  creates a REAP user and records its ID against the vault. 409 if the vault
  already has one.
- `GET /user` — fetches the vault's REAP user. 404 if none exists yet.
- `PUT /user/email` — `{email}`.
- `PUT /user/phone` — `{phoneNumber}`.
- `POST /account` — requires an `Idempotency-Key` header (client-generated,
  forwarded to REAP as-is); `{signers?}`; creates a REAP account owned by the
  vault's REAP user. 404 if the vault has no REAP user yet.
- `GET /account?limit=&cursor=` — lists the vault's REAP accounts
  (`ownerId` filtered to the vault's REAP user). 404 if the vault has no REAP
  user yet.
- `GET /account/signer-message` — generates a message for the client to sign
  when providing `signers` to `POST /account`.
- `GET /account/:id` — fetches a REAP account.
- `GET /account/:id/balance` — fetches a REAP account's balance.
- `GET /account/:id/assets` — fetches a REAP account's assets.

## Commands

- Build: `make build` (or `go build ./...`)
- Test: `make test` (or `go test ./...`)
- Lint: `make lint` (or `golangci-lint run ./...`)
- All of the above: `make ci`
- Local Postgres: `make db-up` / `make db-down`
