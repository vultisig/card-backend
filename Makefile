.PHONY: build test lint ci db-up db-down

build:
	go build ./...

test:
	go test ./...

lint:
	golangci-lint run ./...

ci: build test lint

db-up:
	docker compose up -d

db-down:
	docker compose down
