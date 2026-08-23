# Imagem CloudNativePG (Postgres 17) + extensão pgvector.
#
# A extensão `vector` (pgvector 0.8.x) NÃO é "trusted" por padrão, então só um
# superuser poderia rodar CREATE EXTENSION. Como o role owner criado pelo CNPG
# não é superuser e a app aplica as migrations com esse role, marcamos a extensão
# como trusted aqui — assim o owner consegue criar `vector` sem privilégio de
# superuser. pg_trgm já é trusted no core.
#
# Build & push (ECR, mesma conta/registry das demais apps):
#   aws ecr get-login-password --region sa-east-1 | docker login --username AWS \
#     --password-stdin 718446585908.dkr.ecr.sa-east-1.amazonaws.com
#   docker build -t 718446585908.dkr.ecr.sa-east-1.amazonaws.com/rodolfobonis/lequeplay-cnpg:17-pgvector \
#     deploy/cnpg-pgvector
#   docker push 718446585908.dkr.ecr.sa-east-1.amazonaws.com/rodolfobonis/lequeplay-cnpg:17-pgvector
ARG CNPG_TAG=17.2
FROM ghcr.io/cloudnative-pg/postgresql:${CNPG_TAG}

USER root
RUN set -eux; \
    apt-get update; \
    apt-get install -y --no-install-recommends postgresql-17-pgvector; \
    rm -rf /var/lib/apt/lists/*; \
    echo 'trusted = true' >> /usr/share/postgresql/17/extension/vector.control

# CNPG roda o Postgres como uid 26 (postgres).
USER 26
