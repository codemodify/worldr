.PHONY: build test fmt

BIN_DIR := bin
export CGO_ENABLED ?= 1

build:
	mkdir -p $(BIN_DIR)
	go build -o $(BIN_DIR)/worldr-shell ./cmd/worldr-shell
	go build -o $(BIN_DIR)/worldr-session ./cmd/worldr-session

test:
	go test ./...

fmt:
	gofmt -w .
