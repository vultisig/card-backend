# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project status

This is the Vultisig Card backend service. It has a minimal Echo HTTP server with config loading, standard middleware, a Postgres connection pool, `vault_tokens` and `vultisig_reap_mappings` tables/models, a `/health` route, a nonce-based `/auth` route that issues JWTs, and authenticated `/user` routes that proxy user creation/lookup/email/phone updates to the REAP API.

## Commands

- Build: `make build` (or `go build ./...`)
- Test: `make test` (or `go test ./...`)
- Lint: `make lint` (or `golangci-lint run ./...`)
- All of the above: `make ci`
- Local Postgres: `make db-up` (docker compose, `card_backend` db on `localhost:5432`, user/pass `postgres`) / `make db-down`
- Run: `go run ./cmd/server` (requires Postgres reachable at `DatabaseURL`, or the server fails fast at startup)
- Module management: `go mod tidy`

Go version: 1.26.5 (see `go.mod`).

golangci-lint must be built with a Go toolchain >= the version in `go.mod`, or it refuses to run ("Go language version used to build golangci-lint is lower than the targeted Go version"). If `make lint` fails with that error, `brew upgrade golangci-lint`. CI pins `golangci-lint-action@v7` with `version: v2.12.2` for the same reason — v6 of the action doesn't support golangci-lint v2 at all.

## Architecture

- `cmd/server/main.go` — entrypoint. Loads config, connects to Postgres (fatal on failure), builds the Echo instance with middleware (`Recover`, `RequestLogger`, `RequestID`, `CORS`, `Secure`, `Gzip`), registers routes, starts listening on `cfg.Port`. `/health` pings the DB pool.
- `internal/config/config.go` — config loading via viper. Reads `PORT`, `DATABASE_URL`, `REAP_API_KEY`, and `REAP_ENV` (`sandbox`/`prod`, default `sandbox`) from env (defaults: `8080` and a local `postgres://postgres:postgres@localhost:5432/card_backend?sslmode=disable` for the first two) and optionally merges a `config.json` in the working directory if present; env always takes precedence via `viper.AutomaticEnv()`. `main.go` fails fast at startup if `JWT_SECRET` or `REAP_API_KEY` is unset.
- `internal/db/db.go` — Postgres connection pool via `pgxpool` (jackc/pgx v5). `Connect` opens the pool and pings it before returning.
- `internal/db/migrate.go` — `Migrate` runs idempotent `CREATE TABLE IF NOT EXISTS` DDL for the `vault_tokens` and `vultisig_reap_mappings` tables at startup (raw SQL, not a migration framework — see the `ponytail:` comment there for when to switch to golang-migrate).
- `internal/models/` — DB-backed model structs, one file per table (plain structs with `json` tags only, no ORM tags — the project uses raw SQL via pgxpool, not gorm). New DB models always go here.
- `internal/reapmapping/store.go` — Postgres-backed store for `vultisig_reap_mappings` (`models.VultisigReapMapping`). `GetNonce`/`ClaimNonce` handle the auth nonce (see below). `GetReapUserID` returns a vault's REAP user ID (`""` if unset); `SetReapUserID` sets it, creating the mapping row on first use, and only if it isn't already set (returns `false` if it was — same atomic conditional-update pattern as `ClaimNonce`).
- `internal/reap/client.go` — thin client for the REAP API (`https://sandbox.api.reap.global` / `https://prod.api.reap.global`, chosen by `Config.ReapEnv`). `CreateUser`/`GetUser`/`UpdateEmail`/`UpdatePhoneNumber` send the `Authorization: Bearer` and `Reap-Version` headers and return REAP's raw status/JSON body unchanged (including non-2xx error bodies and REAP's 204-empty-body on a successful update), so callers can pass them straight through.
- `internal/service/auth.go` — `AuthService.Authenticate` backs `POST /auth`: verifies a secp256k1 signature over the client-supplied nonce, checks it against `reapmapping.GetNonce`, claims it via `reapmapping.ClaimNonce` (replay protection), and issues a JWT keyed on the vault's public key. `AuthService.RequireAuth` is Echo middleware that validates the bearer JWT and stores `*Claims` (including the vault's public key) on the Echo context under `"claims"`; it guards the `/user` route group.
- `internal/service/user.go` — `UserService` backs `/user`. `CreateUser` rejects with `ErrReapUserExists` if the vault already has a REAP user ID (no REAP call made); otherwise it calls `reap.Client.CreateUser` and, on a 2xx response, records the returned id via `reapmapping.SetReapUserID`. `GetUser`/`UpdateEmail`/`UpdatePhoneNumber` all resolve the vault's REAP user ID first (shared `reapUserID` helper) and return `ErrNoReapUser` if none is recorded yet, otherwise call the matching `reap.Client` method. Non-2xx REAP responses are returned as-is (no error) for passthrough to the HTTP caller.

## CI

`.github/workflows/ci.yml` runs `build`, `test`, and `lint` as separate jobs on PRs targeting `main`.
