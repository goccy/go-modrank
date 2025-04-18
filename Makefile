export

GOBIN := $(PWD)/bin
PATH := $(GOBIN):$(PATH)

SHELL := env PATH='$(PATH)' bash

.PHONY: tools
tools:
	@go install github.com/golangci/golangci-lint/cmd/golangci-lint@v1.64.8

.PHONY: lint
lint: tools
	$(GOBIN)/golangci-lint run $(args) ./...

.PHONY: test
test:
	go test -v -race ./...

.PHONY: cover
cover:
	go test -coverprofile=cover.out ./...

.PHONY: cover-html
cover-html: cover
	go tool cover -html=cover.out
