# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project status

This is the Vultisig Card backend service, currently just a scaffold: a single `main.go` that logs a startup message. There is no HTTP server, routing, database, or business logic yet. A larger scaffold PR is incoming (see `feat/initial-structure`) — expect this file to need real architecture notes once that lands.

## Commands

- Build: `go build ./...`
- Run: `go run ./cmd/server`
- Vet: `go vet ./...`
- Tests: `go test ./...` (no tests exist yet)
- Module management: `go mod tidy`

Go version: 1.26.5 (see `go.mod`). No external dependencies yet.

## Structure

- `cmd/server/main.go` — entrypoint, currently a stub.
