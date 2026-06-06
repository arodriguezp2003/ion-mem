.DEFAULT_GOAL := help

## build: compile all packages
.PHONY: build
build:
	go build ./...

## test: run all tests
.PHONY: test
test:
	go test ./...

## lint: run go vet on all packages
.PHONY: lint
lint:
	go vet ./...

## fmt: check gofmt formatting (exits non-zero if drift is detected)
.PHONY: fmt
fmt:
	@out="$$(gofmt -l .)" && test -z "$$out" || (echo "$$out" && exit 1)

## help: list available make targets with descriptions
.PHONY: help
help:
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/^## //' | column -t -s ':'
