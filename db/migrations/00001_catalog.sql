-- +goose Up
-- +goose StatementBegin

-- Extensões: vetores (busca semântica) e trigram (busca fuzzy por título).
CREATE EXTENSION IF NOT EXISTS vector;
CREATE EXTENSION IF NOT EXISTS pg_trgm;

-- Gêneros do catálogo (para o endpoint de navegação por gênero).
CREATE TABLE generos (
    id      serial PRIMARY KEY,
    tmdb_id integer UNIQUE,
    nome    text NOT NULL UNIQUE
);

-- Pessoas: elenco e equipe (diretores, atores, apresentadores).
CREATE TABLE pessoas (
    id        uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tmdb_id   integer UNIQUE,
    nome      text NOT NULL,
    foto_path text,
    created_at timestamptz NOT NULL DEFAULT now()
);

-- Mídias: base para filmes, séries e podcasts (discriminador: tipo).
CREATE TABLE midias (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tipo           text NOT NULL CHECK (tipo IN ('filme', 'serie', 'podcast')),
    tmdb_id        integer,
    titulo         text NOT NULL,
    titulo_original text,
    sinopse        text,
    ano            integer,
    generos        text[] NOT NULL DEFAULT '{}',
    poster_path    text,
    duracao_min    integer,
    popularidade   real NOT NULL DEFAULT 0,
    nota_media     real NOT NULL DEFAULT 0,
    -- Vetor de embedding (gemini-embedding-001, 768 dims, cosseno).
    embedding      vector(768),
    -- Coluna full-text gerada (português) sobre título, título original e
    -- sinopse. Usada como perna lexical da busca híbrida. Gêneros não entram
    -- aqui (array_to_string é STABLE, não IMMUTABLE); o filtro por gênero usa a
    -- coluna generos diretamente.
    fts            tsvector GENERATED ALWAYS AS (
        to_tsvector('portuguese'::regconfig,
            coalesce(titulo, '') || ' ' ||
            coalesce(titulo_original, '') || ' ' ||
            coalesce(sinopse, '')
        )
    ) STORED,
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now(),
    -- Um tmdb_id é único por tipo (um filme e uma série podem colidir de id na TMDB).
    UNIQUE (tipo, tmdb_id)
);

-- Temporadas (apenas para séries).
CREATE TABLE temporadas (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    midia_id        uuid NOT NULL REFERENCES midias(id) ON DELETE CASCADE,
    numero          integer NOT NULL,
    nome            text,
    ano             integer,
    total_episodios integer NOT NULL DEFAULT 0,
    UNIQUE (midia_id, numero)
);

-- Episódios (dentro de temporadas).
CREATE TABLE episodios (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    temporada_id uuid NOT NULL REFERENCES temporadas(id) ON DELETE CASCADE,
    numero       integer NOT NULL,
    nome         text,
    sinopse      text,
    duracao_min  integer,
    UNIQUE (temporada_id, numero)
);

-- Créditos: liga pessoas a mídias com um papel (direcao|elenco|apresentacao).
CREATE TABLE creditos (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    midia_id   uuid NOT NULL REFERENCES midias(id) ON DELETE CASCADE,
    pessoa_id  uuid NOT NULL REFERENCES pessoas(id) ON DELETE CASCADE,
    papel      text NOT NULL CHECK (papel IN ('direcao', 'elenco', 'apresentacao')),
    personagem text,
    ordem      integer NOT NULL DEFAULT 0,
    UNIQUE (midia_id, pessoa_id, papel, personagem)
);

-- Versão do catálogo: linha única, incrementada a cada ingestão, para o cliente
-- invalidar cache.
CREATE TABLE catalog_version (
    id         boolean PRIMARY KEY DEFAULT true CHECK (id),
    versao     bigint NOT NULL DEFAULT 1,
    updated_at timestamptz NOT NULL DEFAULT now()
);
INSERT INTO catalog_version (id, versao) VALUES (true, 1);

-- Índices de busca.
CREATE INDEX midias_embedding_hnsw ON midias USING hnsw (embedding vector_cosine_ops);
CREATE INDEX midias_fts_gin ON midias USING gin (fts);
CREATE INDEX midias_titulo_trgm ON midias USING gin (titulo gin_trgm_ops);
CREATE INDEX midias_generos_gin ON midias USING gin (generos);
CREATE INDEX midias_tipo_idx ON midias (tipo);
CREATE INDEX midias_ano_idx ON midias (ano);
CREATE INDEX midias_popularidade_idx ON midias (popularidade DESC);
CREATE INDEX creditos_midia_idx ON creditos (midia_id);
CREATE INDEX creditos_pessoa_idx ON creditos (pessoa_id);
CREATE INDEX temporadas_midia_idx ON temporadas (midia_id);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS creditos;
DROP TABLE IF EXISTS episodios;
DROP TABLE IF EXISTS temporadas;
DROP TABLE IF EXISTS midias;
DROP TABLE IF EXISTS pessoas;
DROP TABLE IF EXISTS generos;
DROP TABLE IF EXISTS catalog_version;
-- +goose StatementEnd
