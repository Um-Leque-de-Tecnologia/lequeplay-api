# Deploy — LequePlay API (cluster rblab)

Deploy via GitOps: **ArgoCD + `rb-stack` (Helm) + CloudNativePG**. Imagens no **ECR**, segredos no **Vault** (via External Secrets), TLS via cert-manager/Traefik, login via **Keycloak**.

> Ordem importa. O push no `k3s-apps` (main) dispara auto-sync do ArgoCD — só faça **depois** de as imagens existirem no ECR, os segredos estarem no Vault e o realm do Keycloak criado. Caso contrário o pod entra em CrashLoop / ImagePull.

## 0. Pré-requisitos (uma vez)

- Repo `Um-Leque-de-Tecnologia/lequeplay-api` no GitHub com os **secrets de Actions**:
  `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, `AWS_REGION` (sa-east-1), `GH_TOKEN` (PAT com acesso ao `RodolfoBonis/k3s-apps`), `ARGOCD_SERVER`, `ARGOCD_TOKEN`.
  As credenciais AWS vivem no Vault: `k3s/backstage/default_secrets`.

## 1. Segredos no Vault (base `k3s/lequeplay-api`)

As chaves do Gemini/TMDB estão no `demo_gdgjp/.env` (mesmas do projeto irmão). O secret do client Keycloak sai do passo 4.

```
# app (injetado como lequeplay-api-secret)
vault kv put k3s/lequeplay-api/prod/app \
  GEMINI_API_KEY=<gemini> \
  TMDB_READ_TOKEN=<tmdb> \
  KEYCLOAK_CLIENT_SECRET=<secret do passo 4> \
  OBJECT_STORE_ACCESS_KEY=<minio> \
  OBJECT_STORE_SECRET_KEY=<minio>

# db (injetado como lequeplay-api-db-secret + owner do CNPG)
vault kv put k3s/lequeplay-api/prod/db \
  DB_DSN='postgres://lequeplay:<senha>@lequeplay-api-postgres-svc:5432/lequeplay?sslmode=require' \
  POSTGRES_USER=lequeplay \
  POSTGRES_PASSWORD=<senha>
```

MinIO: crie o usuário `lequeplay-api` + buckets `lequeplay-wal`/`lequeplay-backups` (policies rw) — como as demais apps (ver `infra-live/live/prod/minio`).

## 2. Imagem CNPG + pgvector (uma vez)

O Postgres precisa da extensão `vector`. Build da imagem custom (Postgres 17 + pgvector marcado como *trusted*) e push no ECR:

```
aws ecr get-login-password --region sa-east-1 | docker login --username AWS \
  --password-stdin 718446585908.dkr.ecr.sa-east-1.amazonaws.com
aws ecr create-repository --repository-name rodolfobonis/lequeplay-cnpg --region sa-east-1 || true
docker build -t 718446585908.dkr.ecr.sa-east-1.amazonaws.com/rodolfobonis/lequeplay-cnpg:17-pgvector deploy/cnpg-pgvector
docker push 718446585908.dkr.ecr.sa-east-1.amazonaws.com/rodolfobonis/lequeplay-cnpg:17-pgvector
```

## 3. Imagem da API

Merge na `main` do `lequeplay-api` → o CI (`.github/workflows`) buildar e empurra `rodolfobonis/lequeplay-api:<versão>` no ECR, faz o bump em `k3s-apps/apps/lequeplay-api/values-prod.yaml` e sincroniza o ArgoCD. (No primeiro deploy, garanta os passos 1, 2 e 4 antes.)

## 4. Realm do Keycloak (infra-live → Atlantis)

`infra-live/live/prod/keycloak/realms/lequeplay/` já define o realm `lequeplay` + client confidencial `lequeplay-api` (Direct Access Grants para o proxy de login + audience mapper). Registrado em `live/prod/keycloak/main.tf`.

Abra o PR no `infra-live`; o Atlantis roda `plan`/`apply`. Depois do apply, capture o secret do client e grave no Vault:

```
terraform output -raw lequeplay_api_client_secret   # → k3s/lequeplay-api/prod/app KEYCLOAK_CLIENT_SECRET
```

## 5. Manifests no k3s-apps (auto-deploy)

Os arquivos já estão em `k3s-apps/`:
- `clusters/prod/lequeplay-api-cnpg-prod.yaml` (wave -1: ExternalSecrets do CNPG)
- `clusters/prod/lequeplay-api-prod.yaml` (app: web-server + CNPG)
- `clusters/prod/lequeplay-api-monitoring-prod.yaml` (ServiceMonitor)
- `apps/lequeplay-api/**`

Commit + push na `main` do `k3s-apps` → o app-of-apps (`clusters-prod`) descobre e o ArgoCD sincroniza. A API roda as migrations no boot (advisory lock; `CREATE EXTENSION vector` funciona porque a imagem CNPG marca a extensão como trusted).

## 6. Popular o catálogo

Rode o `cmd/seed` uma vez contra o Postgres do cluster (Job pontual ou port-forward), com `TMDB_READ_TOKEN` + `GEMINI_API_KEY` + `DB_DSN` do cluster. Ex.:

```
kubectl -n lequeplay-api run lequeplay-seed --rm -it --restart=Never \
  --image=718446585908.dkr.ecr.sa-east-1.amazonaws.com/rodolfobonis/lequeplay-api:<versão> \
  --env=DB_DSN=... --env=TMDB_READ_TOKEN=... --env=GEMINI_API_KEY=... \
  --command -- /app   # (se buildar a imagem seed com BIN=seed) 
```

(Ou build de uma imagem `BIN=seed` e um `Job` dedicado.)

## 7. Verificação

```
curl https://api.lequeplay.rodolfodebonis.com.br/healthz            # ok
curl 'https://api.lequeplay.rodolfodebonis.com.br/v1/busca?q=detetive+que+resolve+crimes'
```
- Pod `Ready`, TLS válido (cert-manager), `/readyz` 200, `/metrics` raspado no Prometheus.
- Login: `POST /v1/auth/login {usuario,senha}` de um usuário do realm `lequeplay` retorna tokens; rota protegida rejeita Bearer inválido (401 problem+json).
