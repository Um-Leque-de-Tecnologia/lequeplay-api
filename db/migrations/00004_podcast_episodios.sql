-- +goose Up
-- +goose StatementBegin

-- frequencia: periodicidade do podcast (ex.: "Semanal"); só faz sentido para
-- tipo='podcast'. NULLABLE porque filme e série não têm.
ALTER TABLE midias ADD COLUMN frequencia text;

-- IDs de coleção da Apple (fonte de podcasts) podem exceder int32; bigint acomoda
-- tanto o tmdb_id de filmes/séries quanto o collectionId de podcasts.
ALTER TABLE midias ALTER COLUMN tmdb_id TYPE bigint;

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
ALTER TABLE midias DROP COLUMN IF EXISTS frequencia;
-- tmdb_id permanece bigint no rollback: reverter para integer estouraria em IDs
-- de podcast já gravados. bigint é superconjunto seguro de integer.
-- +goose StatementEnd
