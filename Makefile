.PHONY: build test test-docker lint proto e2e mod-check

# Modules Go du monorepo, un par go.mod (cœur, sdk, modules/*, test/modules/*).
# Découverts automatiquement : ajouter un module ne doit nécessiter aucune
# modification hors de son répertoire (docs/02-architecture.md), donc pas de
# liste codée en dur ici — voir genesis modules scaffold (docs/10).
GO_MODULES := $(shell find . -name go.mod -not -path './.git/*' -exec dirname {} \; | sed 's|^\./||' | sort)

build:
	go build ./...

# Tests unitaires : sans réseau ni démon de conteneurs.
test:
	@set -e; for m in $(GO_MODULES); do \
		echo "==> go test ($$m)"; \
		(cd $$m && go test ./...); \
	done

# Tests d'intégration contre de vrais conteneurs (build tag `docker`) :
# nécessitent un démon docker ou podman et tirent des images.
test-docker:
	@set -e; for m in $(GO_MODULES); do \
		echo "==> go test -tags docker ($$m)"; \
		(cd $$m && go test -tags docker ./...); \
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

# Le protocole module/v1 (sdk/proto) existe depuis le jalon J3 (docs/08-jalons.md).
proto:
	@if [ -z "$$(find sdk/proto -name '*.proto' 2>/dev/null)" ]; then \
		echo "aucun fichier .proto pour le moment"; \
	else \
		cd sdk/proto && buf generate; \
	fi

# Nécessite un Proxmox : ne jamais lancer sans demande explicite (CLAUDE.md).
e2e:
	go test -tags integration ./test/e2e/...
