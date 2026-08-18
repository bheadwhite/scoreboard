# Scoreboard — family game records.
#
# Requires Go 1.25+, Node 22+, pnpm, and Postgres. Codegen additionally needs
# the Go protoc plugins on PATH (see `make tools`); buf itself comes from
# node_modules, so no global install is needed.

-include .env.local
export

DATABASE_URL ?= postgres://$(USER)@localhost:5432/scoreboard_dev?sslmode=disable
PORT         ?= 8080
BUF          := ./node_modules/.bin/buf
GOBIN        := $(shell go env GOPATH)/bin

.PHONY: help
help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN{FS=":.*?## "}{printf "  \033[36m%-16s\033[0m %s\n", $$1, $$2}'

.PHONY: setup
setup: tools install db ## One-time setup: tools, deps, and a local database

.PHONY: tools
tools: ## Install the Go protobuf plugins and the live-reload runner
	go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.36.10
	go install connectrpc.com/connect/cmd/protoc-gen-connect-go@v1.19.1
	go install github.com/air-verse/air@latest

.PHONY: install
install: ## Install JS dependencies
	pnpm install

.PHONY: db
db: ## Create the local development database
	createdb scoreboard_dev 2>/dev/null || echo "scoreboard_dev already exists"

.PHONY: generate
generate: ## Regenerate Go + TypeScript from the protos
	PATH="$(PATH):$(GOBIN)" $(BUF) lint
	PATH="$(PATH):$(GOBIN)" $(BUF) generate

.PHONY: dev-api
dev-api: ## Run the backend with live reload (applies migrations on start)
	@cd backend && if [ -x "$(GOBIN)/air" ]; then \
		echo "▸ live reload on — Go changes rebuild automatically"; \
		"$(GOBIN)/air"; \
	else \
		echo "▸ no live reload (run 'make tools' to install air); restart after Go changes"; \
		go run ./cmd/server; \
	fi

.PHONY: dev-web
dev-web: ## Run the Vite dev server, proxying /api to the backend
	pnpm --filter @scoreboard/frontend dev

.PHONY: build
build: ## Build the frontend and the server binary
	pnpm --filter @scoreboard/frontend build
	cd backend && CGO_ENABLED=0 go build -trimpath -o server ./cmd/server

.PHONY: run
run: build ## Build everything and serve it from the Go binary
	cd backend && STATIC_DIR=../frontend/dist ./server

.PHONY: test
test: ## Run all tests
	cd backend && go test ./...
	pnpm --filter @scoreboard/frontend test

.PHONY: check
check: ## Typecheck and test everything
	cd backend && go vet ./... && go test ./...
	pnpm --filter @scoreboard/frontend check

.PHONY: data
data: ## Show what is in the database (families, matches, players)
	cd backend && go run ./cmd/reset

.PHONY: clear-family
clear-family: ## Delete one family and everything under it: make clear-family FAMILY="The Whites"
	@test -n "$(FAMILY)" || (echo 'set FAMILY="<name or id>"' && exit 1)
	cd backend && go run ./cmd/reset -family "$(FAMILY)" -users -yes

.PHONY: clear-all
clear-all: ## Delete every family, match and player (keeps the game catalog)
	cd backend && go run ./cmd/reset -all -yes

.PHONY: consolidate
consolidate: ## Show the plan for folding every family into one: make consolidate [INTO="The Whites"]
	cd backend && go run ./cmd/consolidate -into "$(INTO)"

.PHONY: consolidate-apply
consolidate-apply: ## Actually fold every family into one: make consolidate-apply [INTO="The Whites"]
	cd backend && go run ./cmd/consolidate -into "$(INTO)" -yes

.PHONY: sweep
sweep: ## Finalize any claims whose concession window has closed
	curl -fsS -X POST localhost:$(PORT)/tasks/sweep -H "Authorization: Bearer $(SWEEP_TOKEN)"

.PHONY: deploy
deploy: ## Deploy to Cloud Run (see DEPLOY.md)
	./scripts/deploy.sh
