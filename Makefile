.PHONY: build test lint proto e2e

# Modules Go du monorepo (chacun a son propre go.mod). modules/* et
# test/modules/* s'ajouteront à mesure qu'ils sont créés (docs/02-architecture.md).
GO_MODULES := . sdk

build:
	go build ./...

test:
	@set -e; for m in $(GO_MODULES); do \
		echo "==> go test ($$m)"; \
		(cd $$m && go test ./...); \
	done

lint:
	@set -e; for m in $(GO_MODULES); do \
		echo "==> golangci-lint ($$m)"; \
		(cd $$m && golangci-lint run ./...); \
	done

# Le protocole module/v1 (sdk/proto) arrive au jalon J3 (docs/08-jalons.md).
# En attendant, ce target ne fait rien si aucun .proto n'existe encore.
proto:
	@if [ -z "$$(find sdk/proto -name '*.proto' 2>/dev/null)" ]; then \
		echo "aucun fichier .proto pour le moment (protocole module/v1 prévu au jalon J3)"; \
	else \
		cd sdk/proto && buf generate; \
	fi

# Nécessite un Proxmox : ne jamais lancer sans demande explicite (CLAUDE.md).
e2e:
	go test -tags integration ./test/e2e/...
