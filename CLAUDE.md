# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project status

This is the Vultisig Card backend service. It currently has a minimal Echo HTTP server with config loading, standard middleware, a Postgres connection pool, and a `cards` table/model, but no routes beyond `/health` and no CRUD/business logic yet.

## Commands

- Build: `make build` (or `go build ./...`)
- Test: `make test` (or `go test ./...`, no tests exist yet)
- Lint: `make lint` (or `golangci-lint run ./...`)
- All of the above: `make ci`
- Local Postgres: `make db-up` (docker compose, `card_backend` db on `localhost:5432`, user/pass `postgres`) / `make db-down`
- Run: `go run ./cmd/server` (requires Postgres reachable at `DatabaseURL`, or the server fails fast at startup)
- Module management: `go mod tidy`

Go version: 1.26.5 (see `go.mod`).

golangci-lint must be built with a Go toolchain >= the version in `go.mod`, or it refuses to run ("Go language version used to build golangci-lint is lower than the targeted Go version"). If `make lint` fails with that error, `brew upgrade golangci-lint`. CI pins `golangci-lint-action@v7` with `version: v2.12.2` for the same reason — v6 of the action doesn't support golangci-lint v2 at all.

## Architecture

- `cmd/server/main.go` — entrypoint. Loads config, connects to Postgres (fatal on failure), builds the Echo instance with middleware (`Recover`, `RequestLogger`, `RequestID`, `CORS`, `Secure`, `Gzip`), registers routes, starts listening on `cfg.Port`. `/health` pings the DB pool.
- `internal/config/config.go` — config loading via viper. Reads `PORT` and `DATABASE_URL` from env (defaults: `8080`, and a local `postgres://postgres:postgres@localhost:5432/card_backend?sslmode=disable`) and optionally merges a `config.json` in the working directory if present; env always takes precedence via `viper.AutomaticEnv()`.
- `internal/db/db.go` — Postgres connection pool via `pgxpool` (jackc/pgx v5). `Connect` opens the pool and pings it before returning.
- `internal/db/migrate.go` — `Migrate` runs idempotent `CREATE TABLE IF NOT EXISTS` DDL for the `cards` table at startup (raw SQL, not a migration framework — see the `ponytail:` comment there for when to switch to golang-migrate).
- `internal/card/card.go` — `Card` model (`CardID`, `VaultPublicKeyECDSA`, `CardTier`, `InitiateDate`, `IsActive`), mirrors the `cards` table. `card_id` is the primary key; `vault_public_key_ecdsa` is indexed for lookups by vault.

## CI

`.github/workflows/ci.yml` runs `build`, `test`, and `lint` as separate jobs on PRs targeting `main`.
