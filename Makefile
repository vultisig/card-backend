.PHONY: build test lint vet run tidy

build:
	go build ./...

test:
	go test -race ./...

vet:
	go vet ./...

lint:
	golangci-lint run ./...

run:
	go run ./cmd/card-backend

tidy:
	go mod tidy
