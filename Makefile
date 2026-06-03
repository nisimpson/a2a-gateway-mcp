# Makefile for a2a-gateway-mcp

pkg?=a2a-gateway-mcp

# ==================================================================================== #
# HELPERS
# ==================================================================================== #

## help: print this help message
.PHONY: help
help:
	@echo 'Usage:'
	@sed -n 's/^##//p' ${MAKEFILE_LIST} | column -t -s ':' |  sed -e 's/^/ /'

# ==================================================================================== #
# BUILD
# ==================================================================================== #

## build: build all binaries
.PHONY: all build gateway mockserver mockserver-minimal mockserver-knock-knock
all: build

build: gateway mockserver

gateway:
	go build -o bin/a2a-gateway-mcp ./cmd/a2a-gateway-mcp

mockserver: mockserver-minimal mockserver-knock-knock

mockserver-minimal:
	go build -tags dev -o bin/mockserver-minimal ./cmd/mockserver/minimal

mockserver-knock-knock:
	go build -tags dev -o bin/mockserver-knock-knock ./cmd/mockserver/knock-knock

## clean: remove build artifacts
.PHONY: clean
clean:
	rm -rf bin

# ==================================================================================== #
# QUALITY CONTROL
# ==================================================================================== #

## tidy: format code and tidy modfile
.PHONY: tidy
tidy:
	go fmt ./...
	go mod tidy -v

## audit: run quality control checks
.PHONY: audit
audit:
	go mod verify
	go vet ./...
	go run github.com/golangci/golangci-lint/cmd/golangci-lint@latest run
	go run golang.org/x/vuln/cmd/govulncheck@latest ./...
	go test -race -buildvcs -vet=off ./...

.PHONY: no-dirty
no-dirty:
	git diff --exit-code

## lint: run audit, tidy, and verify no uncommitted changes
.PHONY: lint
lint: audit tidy no-dirty

# ==================================================================================== #
# TESTING
# ==================================================================================== #

## test: run all tests
.PHONY: test
test:
	go test -cover ./... -count=1

## test-cover: run tests with coverage report in browser
.PHONY: test-cover
test-cover:
	go test -coverprofile=/tmp/$(pkg).coverage.out ./...
	go tool cover -html=/tmp/$(pkg).coverage.out

# ==================================================================================== #
# OPERATIONS
# ==================================================================================== #

## push: push changes to the remote Git repository
.PHONY: push
push: lint
	git push
