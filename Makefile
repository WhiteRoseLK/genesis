.PHONY: build test test-race test-docker pull-images lint proto proto-check e2e mod-check vuln licenses tools

# Go modules of the monorepo, one per go.mod (core, sdk, modules/*, test/modules/*).
# Discovered automatically: adding a module must not require any change outside
# its directory (docs/02-architecture.md), hence no hard-coded list here — see
# genesis modules scaffold (docs/10).
GO_MODULES := $(shell find . -name go.mod -not -path './.git/*' -exec dirname {} \; | sed 's|^\./||' | sort)

# Development tools, pinned versions (installed by `make tools` into .bin/,
# which takes precedence over the PATH).
GOLANGCI_LINT_VERSION   := v2.13.2
BUF_VERSION             := v1.73.0
PROTOC_GEN_GO_VERSION   := v1.36.12
PROTOC_GEN_GRPC_VERSION := v1.6.2
GOVULNCHECK_VERSION     := v1.8.0
GO_LICENSES_VERSION     := v2.0.1
TOOLS_BIN := $(CURDIR)/.bin
# Tools are built with the project's Go toolchain (go.mod), not the system's:
# golangci-lint and govulncheck must understand the targeted Go version.
GO_TOOLCHAIN := $(shell go env GOVERSION)
export PATH := $(TOOLS_BIN):$(PATH)

# Dependency licenses compatible with Apache-2.0 (docs/09-decisions.md, ADR-009).
ALLOWED_LICENSES := Apache-2.0,BSD-2-Clause,BSD-3-Clause,MIT,ISC,MPL-2.0

# Shipped binaries: CGO_ENABLED=0, linux/amd64 and linux/arm64
# (ex. `make build GOARCH=arm64`).
GOOS   ?= linux
GOARCH ?= amd64

tools:
	GOTOOLCHAIN=$(GO_TOOLCHAIN) GOBIN=$(TOOLS_BIN) go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)
	GOTOOLCHAIN=$(GO_TOOLCHAIN) GOBIN=$(TOOLS_BIN) go install github.com/bufbuild/buf/cmd/buf@$(BUF_VERSION)
	GOTOOLCHAIN=$(GO_TOOLCHAIN) GOBIN=$(TOOLS_BIN) go install google.golang.org/protobuf/cmd/protoc-gen-go@$(PROTOC_GEN_GO_VERSION)
	GOTOOLCHAIN=$(GO_TOOLCHAIN) GOBIN=$(TOOLS_BIN) go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@$(PROTOC_GEN_GRPC_VERSION)
	GOTOOLCHAIN=$(GO_TOOLCHAIN) GOBIN=$(TOOLS_BIN) go install golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION)
	GOTOOLCHAIN=$(GO_TOOLCHAIN) GOBIN=$(TOOLS_BIN) go install github.com/google/go-licenses/v2@$(GO_LICENSES_VERSION)

# -o /dev/null: checks that the code compiles without leaving a binary in the
# module's directory (a module = a main package, which `go build ./...` would
# write under the module's name).
build:
	@set -e; for m in $(GO_MODULES); do \
		echo "==> go build $(GOOS)/$(GOARCH) ($$m)"; \
		(cd $$m && CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) go build -o /dev/null ./...); \
	done

# Unit tests: no network, no container daemon.
test:
	@set -e; for m in $(GO_MODULES); do \
		echo "==> go test ($$m)"; \
		(cd $$m && go test ./...); \
	done

# Same tests with the race detector: requires CGO, unlike the shipped
# binaries (CGO_ENABLED=0); only affects how the tests run.
test-race:
	@set -e; for m in $(GO_MODULES); do \
		echo "==> go test -race ($$m)"; \
		(cd $$m && CGO_ENABLED=1 go test -race ./...); \
	done

# Integration tests against real containers (`docker` build tag): need a
# docker or podman daemon and pull images.
test-docker:
	@set -e; for m in $(GO_MODULES); do \
		echo "==> go test -tags docker ($$m)"; \
		(cd $$m && go test -tags docker ./...); \
	done

# Pinned images (repo:tag@sha256:…) found in the Go code: no list to
# maintain, a new module is covered automatically. Pulled ahead of time with
# spaced-out retries: a registry's temporary rate limit (toomanyrequests) no
# longer fails the integration tests, and a registry that stays unavailable
# is reported as such.
CONTAINER_RUNTIME ?= docker
PULL_ATTEMPTS     ?= 5
PINNED_IMAGES = $(shell grep -rhoE '"[a-z0-9./-]+:[A-Za-z0-9._-]+@sha256:[0-9a-f]{64}"' --include=*.go . | tr -d '"' | sort -u)

pull-images:
	@set -e; for img in $(PINNED_IMAGES); do \
		i=1; \
		until $(CONTAINER_RUNTIME) pull -q $$img >/dev/null; do \
			if [ $$i -ge $(PULL_ATTEMPTS) ]; then \
				echo "failed: $$img unreachable after $(PULL_ATTEMPTS) attempts (registry unavailable or rate limited)" >&2; \
				exit 1; \
			fi; \
			echo "==> retrying in $$((i * 30)) s: $$img"; \
			sleep $$((i * 30)); i=$$((i + 1)); \
		done; \
		echo "==> $$img"; \
	done

# Each go.mod must be self-contained (outside the go.work workspace) and up to
# date (go mod tidy): a third-party module or `go install` has no go.work.
mod-check:
	@set -e; for m in $(GO_MODULES); do \
		echo "==> go mod tidy -diff + build hors workspace ($$m)"; \
		(cd $$m && GOWORK=off go mod tidy -diff && GOWORK=off go build -o /dev/null ./...); \
	done

lint:
	@set -e; for m in $(GO_MODULES); do \
		echo "==> golangci-lint ($$m)"; \
		(cd $$m && golangci-lint run ./...); \
	done

# The module/v1 protocol (sdk/proto) exists since milestone M3 (docs/08-milestones.md).
proto:
	@if [ -z "$$(find sdk/proto -name '*.proto' 2>/dev/null)" ]; then \
		echo "no .proto file yet"; \
	else \
		cd sdk/proto && buf generate; \
	fi

# The generated code is committed: it must match the .proto files (buf lint +
# regeneration with no diff).
proto-check: proto
	cd sdk/proto && buf lint
	git diff --exit-code -- sdk/go/gen

# Known vulnerabilities reachable from the code (Go vulnerability database).
vuln:
	@set -e; for m in $(GO_MODULES); do \
		echo "==> govulncheck ($$m)"; \
		(cd $$m && GOWORK=off govulncheck ./...); \
	done

# Third-party dependency licenses (the repository's own packages are ignored).
licenses:
	@set -e; for m in $(GO_MODULES); do \
		echo "==> go-licenses ($$m)"; \
		out=$$(cd $$m && GOWORK=off go-licenses check ./... --ignore github.com/WhiteRoseLK/genesis \
			--allowed_licenses=$(ALLOWED_LICENSES) 2>&1) || { echo "$$out" | grep -v '^W0\|\.s$$'; exit 1; }; \
	done

# Needs a Proxmox: never run without an explicit request (CLAUDE.md).
e2e:
	go test -tags integration ./test/e2e/...
