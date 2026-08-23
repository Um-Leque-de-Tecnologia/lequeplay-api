.DEFAULT_GOAL := help
SHELL := /bin/bash

ifneq (,$(wildcard .env))
include .env
export
endif

BIN_DIR := bin
PKG := github.com/Um-Leque-de-Tecnologia/lequeplay-api
GOBIN := $(shell go env GOPATH)/bin

.PHONY: help
help: ## Lista os alvos disponíveis
	@grep -E '^[a-zA-Z0-9_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}'

.PHONY: tools
tools: ## Instala as ferramentas de codegen (goose)
	go install github.com/pressly/goose/v3/cmd/goose@latest

.PHONY: build
build: ## Compila os binários api e seed
	mkdir -p $(BIN_DIR)
	go build -o $(BIN_DIR)/api ./cmd/api
	go build -o $(BIN_DIR)/seed ./cmd/seed

.PHONY: run
run: ## Sobe a API localmente
	go run ./cmd/api

.PHONY: seed
seed: ## Roda o pipeline de seed (TMDB -> Gemini -> Postgres)
	go run ./cmd/seed

.PHONY: test
test: ## Roda os testes (unit + integração)
	go test ./... -race -count=1

.PHONY: test-unit
test-unit: ## Roda só os testes unitários (sem integração)
	go test ./... -race -count=1 -short

.PHONY: lint
lint: ## Roda o golangci-lint
	$(GOBIN)/golangci-lint run ./...

.PHONY: fmt
fmt: ## Formata e analisa o código
	go fmt ./...
	go vet ./...

.PHONY: tidy
tidy: ## Atualiza go.mod/go.sum
	go mod tidy

.PHONY: deps-up
deps-up: ## Sobe o Postgres (com pgvector) de desenvolvimento
	docker compose up -d

.PHONY: deps-down
deps-down: ## Derruba as dependências de desenvolvimento
	docker compose down

.PHONY: migrate-up
migrate-up: ## Aplica as migrations manualmente
	$(GOBIN)/goose -dir db/migrations postgres "$(DB_DSN)" up
