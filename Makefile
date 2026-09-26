.PHONY: build test test-race lint proto e2e

# Modules Go du monorepo, un par go.mod (cœur, sdk, modules/*, test/modules/*).
# Découverts automatiquement : ajouter un module ne doit nécessiter aucune
# modification hors de son répertoire (docs/02-architecture.md), donc pas de
# liste codée en dur ici — voir genesis modules scaffold (docs/10).
GO_MODULES := $(shell find . -name go.mod -not -path './.git/*' -exec dirname {} \; | sed 's|^\./||' | sort)

build:
	go build ./...

test:
	@set -e; for m in $(GO_MODULES); do \
		echo "==> go test ($$m)"; \
		(cd $$m && go test ./...); \
	done

# Détecteur de concurrence : exige CGO, contrairement aux binaires livrés
# (CGO_ENABLED=0) ; ne concerne que l'exécution des tests.
test-race:
	@set -e; for m in $(GO_MODULES); do \
		echo "==> go test -race ($$m)"; \
		(cd $$m && CGO_ENABLED=1 go test -race ./...); \
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
