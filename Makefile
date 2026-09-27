.PHONY: build test test-race test-docker pull-images lint proto proto-check e2e mod-check vuln licenses tools

# Modules Go du monorepo, un par go.mod (cœur, sdk, modules/*, test/modules/*).
# Découverts automatiquement : ajouter un module ne doit nécessiter aucune
# modification hors de son répertoire (docs/02-architecture.md), donc pas de
# liste codée en dur ici — voir genesis modules scaffold (docs/10).
GO_MODULES := $(shell find . -name go.mod -not -path './.git/*' -exec dirname {} \; | sed 's|^\./||' | sort)

# Outils de développement, versions épinglées (installés par `make tools`
# dans .bin/, prioritaire sur le PATH).
GOLANGCI_LINT_VERSION   := v2.13.2
BUF_VERSION             := v1.73.0
PROTOC_GEN_GO_VERSION   := v1.36.12
PROTOC_GEN_GRPC_VERSION := v1.6.2
GOVULNCHECK_VERSION     := v1.8.0
GO_LICENSES_VERSION     := v2.0.1
TOOLS_BIN := $(CURDIR)/.bin
# Outils compilés avec la chaîne Go du projet (go.mod), pas celle du système :
# golangci-lint et govulncheck doivent comprendre la version de Go ciblée.
GO_TOOLCHAIN := $(shell go env GOVERSION)
export PATH := $(TOOLS_BIN):$(PATH)

# Licences de dépendances compatibles avec Apache-2.0 (docs/09-decisions.md, ADR-009).
ALLOWED_LICENSES := Apache-2.0,BSD-2-Clause,BSD-3-Clause,MIT,ISC,MPL-2.0

# Binaires livrés : CGO_ENABLED=0, linux/amd64 et linux/arm64
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

# -o /dev/null : vérifie la compilation sans déposer de binaire dans le
# répertoire du module (un module = un paquet main, que `go build ./...`
# écrirait sous le nom du module).
build:
	@set -e; for m in $(GO_MODULES); do \
		echo "==> go build $(GOOS)/$(GOARCH) ($$m)"; \
		(cd $$m && CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) go build -o /dev/null ./...); \
	done

# Tests unitaires : sans réseau ni démon de conteneurs.
test:
	@set -e; for m in $(GO_MODULES); do \
		echo "==> go test ($$m)"; \
		(cd $$m && go test ./...); \
	done

# Mêmes tests avec le détecteur de concurrence : exige CGO, contrairement aux
# binaires livrés (CGO_ENABLED=0) ; ne concerne que l'exécution des tests.
test-race:
	@set -e; for m in $(GO_MODULES); do \
		echo "==> go test -race ($$m)"; \
		(cd $$m && CGO_ENABLED=1 go test -race ./...); \
	done

# Tests d'intégration contre de vrais conteneurs (build tag `docker`) :
# nécessitent un démon docker ou podman et tirent des images.
test-docker:
	@set -e; for m in $(GO_MODULES); do \
		echo "==> go test -tags docker ($$m)"; \
		(cd $$m && go test -tags docker ./...); \
	done

# Images épinglées (dépôt:tag@sha256:…) trouvées dans le code Go : aucune
# liste à maintenir, un nouveau module est couvert d'office. Tirées d'avance
# avec reprises espacées : une limite de débit passagère d'un registre
# (toomanyrequests) ne fait plus échouer les tests d'intégration, et un
# registre durablement indisponible est signalé comme tel.
CONTAINER_RUNTIME ?= docker
PULL_ATTEMPTS     ?= 5
PINNED_IMAGES = $(shell grep -rhoE '"[a-z0-9./-]+:[A-Za-z0-9._-]+@sha256:[0-9a-f]{64}"' --include=*.go . | tr -d '"' | sort -u)

pull-images:
	@set -e; for img in $(PINNED_IMAGES); do \
		i=1; \
		until $(CONTAINER_RUNTIME) pull -q $$img >/dev/null; do \
			if [ $$i -ge $(PULL_ATTEMPTS) ]; then \
				echo "échec : $$img inaccessible après $(PULL_ATTEMPTS) essais (registre indisponible ou limite de débit)" >&2; \
				exit 1; \
			fi; \
			echo "==> nouvel essai dans $$((i * 30)) s : $$img"; \
			sleep $$((i * 30)); i=$$((i + 1)); \
		done; \
		echo "==> $$img"; \
	done

# Chaque go.mod doit se suffire à lui-même (hors espace de travail go.work) et
# être à jour (go mod tidy) : un module tiers ou `go install` n'a pas go.work.
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

# Le protocole module/v1 (sdk/proto) existe depuis le jalon J3 (docs/08-milestones.md).
proto:
	@if [ -z "$$(find sdk/proto -name '*.proto' 2>/dev/null)" ]; then \
		echo "aucun fichier .proto pour le moment"; \
	else \
		cd sdk/proto && buf generate; \
	fi

# Le code généré est commité : il doit correspondre aux .proto (buf lint +
# régénération sans écart).
proto-check: proto
	cd sdk/proto && buf lint
	git diff --exit-code -- sdk/go/gen

# Vulnérabilités connues atteignables depuis le code (base de données Go).
vuln:
	@set -e; for m in $(GO_MODULES); do \
		echo "==> govulncheck ($$m)"; \
		(cd $$m && GOWORK=off govulncheck ./...); \
	done

# Licences des dépendances tierces (les paquets du dépôt sont ignorés).
licenses:
	@set -e; for m in $(GO_MODULES); do \
		echo "==> go-licenses ($$m)"; \
		out=$$(cd $$m && GOWORK=off go-licenses check ./... --ignore github.com/WhiteRoseLK/genesis \
			--allowed_licenses=$(ALLOWED_LICENSES) 2>&1) || { echo "$$out" | grep -v '^W0\|\.s$$'; exit 1; }; \
	done

# Nécessite un Proxmox : ne jamais lancer sans demande explicite (CLAUDE.md).
e2e:
	go test -tags integration ./test/e2e/...
