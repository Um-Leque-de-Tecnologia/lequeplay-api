-- +goose Up
-- +goose StatementBegin

-- unaccent para gerar slugs legíveis a partir de títulos acentuados.
CREATE EXTENSION IF NOT EXISTS unaccent;

-- slug: identificador estável e amigável para URLs (usado no detalhe além do UUID).
-- frequencia: periodicidade do podcast (ex.: "Semanal"); só faz sentido para tipo='podcast'.
ALTER TABLE midias ADD COLUMN slug text;
ALTER TABLE midias ADD COLUMN frequencia text;

-- IDs de coleção da Apple (fonte de podcasts) podem exceder int32; bigint acomoda
-- tanto tmdb_id de filmes/séries quanto o collectionId de podcasts.
ALTER TABLE midias ALTER COLUMN tmdb_id TYPE bigint;

-- Backfill: slugifica o título e desempata duplicatas com um sufixo do id, para
-- não violar a unicidade. unaccent é STABLE, então roda dentro do UPDATE normal.
WITH base AS (
    SELECT
        id,
        trim(both '-' FROM regexp_replace(lower(unaccent(titulo)), '[^a-z0-9]+', '-', 'g')) AS s
    FROM midias
),
numbered AS (
    SELECT
        id,
        CASE WHEN s = '' THEN 'midia' ELSE s END AS s,
        row_number() OVER (
            PARTITION BY CASE WHEN s = '' THEN 'midia' ELSE s END ORDER BY id
        ) AS rn
    FROM base
)
UPDATE midias m
SET slug = CASE
        WHEN n.rn = 1 THEN n.s
        ELSE n.s || '-' || substr(m.id::text, 1, 8)
    END
FROM numbered n
WHERE n.id = m.id;

CREATE UNIQUE INDEX midias_slug_uidx ON midias (slug);

-- Episódios de podcast: ligados direto à mídia (podcasts não têm temporadas) e
-- com data de publicação — semântica distinta da tabela episodios (séries).
CREATE TABLE podcast_episodios (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    midia_id     uuid NOT NULL REFERENCES midias(id) ON DELETE CASCADE,
    numero       integer,
    titulo       text NOT NULL,
    duracao_min  integer,
    publicado_em date,
    UNIQUE (midia_id, numero)
);
CREATE INDEX podcast_episodios_midia_idx ON podcast_episodios (midia_id);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS podcast_episodios;
DROP INDEX IF EXISTS midias_slug_uidx;
ALTER TABLE midias DROP COLUMN IF EXISTS frequencia;
ALTER TABLE midias DROP COLUMN IF EXISTS slug;
-- +goose StatementEnd
