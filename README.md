# LequePlay API

API em Go da **LequePlay** — plataforma social para descobrir, acompanhar e avaliar filmes, séries e podcasts. Este serviço entrega o **catálogo público** e a **busca semântica** (feita no nosso backend com embeddings do Gemini + pgvector), além do **login via Keycloak**.

## Arquitetura

- **HTTP**: [chi](https://github.com/go-chi/chi) · **DB**: [pgx](https://github.com/jackc/pgx) + Postgres (pgvector) · **Migrations**: [goose](https://github.com/pressly/goose) (embutidas, advisory lock)
- **Config**: env via [caarlos0/env](https://github.com/caarlos0/env) · **Logs**: `slog` (JSON) · **Métricas**: Prometheus (`/metrics`) · **Tracing**: OpenTelemetry (OTLP)
- **Auth**: proxy de login no Keycloak (password grant) + validação de JWT via JWKS ([go-oidc](https://github.com/coreos/go-oidc))
- **Busca**: embeddings **Gemini** (`gemini-embedding-001`, 768-dim) em **pgvector** (HNSW/cosseno), fundidos com FTS do Postgres via **RRF**; rerank opcional via Gemini LLM
- **Catálogo**: ingestão da **TMDB** (filmes + séries, pt-BR)

```
cmd/
  api/    # servidor HTTP
  seed/   # pipeline TMDB -> Gemini -> Postgres
internal/
  catalog/       # domínio do catálogo + busca (handlers, repo, engine)
  auth/          # proxy de login Keycloak + middleware OIDC
  integrations/  # gemini (embeddings/rerank), tmdb (ingestão)
  platform/      # config, postgres, telemetry, metrics, health, httpserver, apperr
db/migrations/   # SQL (goose)
deploy/          # imagem CNPG + pgvector
```

## Rodando localmente

```bash
cp .env.example .env               # ajuste GEMINI_API_KEY / TMDB_READ_TOKEN
make deps-up                       # Postgres (pgvector) via Docker
make migrate-up                    # aplica as migrations (ou deixe DB_MIGRATE_ON_START=true)
make seed                          # popula o catálogo (SEED_MOVIES/SEED_SERIES)
make run                           # sobe a API em :8080
```

## Endpoints (v1)

| Método | Rota | Descrição |
|--------|------|-----------|
| GET | `/healthz` `/readyz` | liveness / readiness |
| GET | `/metrics` | métricas Prometheus |
| GET | `/v1/generos` | lista de gêneros |
| GET | `/v1/midias` | catálogo (filtros: `tipo`, `genero`, `ano`, `limite`, `offset`) |
| GET | `/v1/midias/{id}` | detalhe (créditos + temporadas) |
| GET | `/v1/busca` | busca (`q`, `modo=auto\|hybrid\|vector\|fts`, filtros) |
| GET | `/v1/catalogo/versao` | versão do catálogo (cache) |
| POST | `/v1/auth/login` | `{usuario, senha}` → tokens (proxy Keycloak) |
| POST | `/v1/auth/refresh` | `{refreshToken}` → tokens |
| POST | `/v1/auth/logout` | `{refreshToken}` → 204 |
| GET | `/v1/auth/me` | claims do token (requer Bearer) |

No modo `KEYCLOAK_MODE=dev`, a autenticação usa headers `X-Debug-Subject`/`X-Debug-Email`/`X-Debug-Roles`.

## Deploy

Ver [DEPLOY.md](./DEPLOY.md) — build da imagem (ECR), CNPG com pgvector, ArgoCD (`rb-stack`), Keycloak e segredos no Vault.
