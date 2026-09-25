-- +goose Up
-- +goose StatementBegin

-- Remove a unicidade por (midia_id, numero): número de episódio de podcast NÃO é
-- confiável como chave — feeds RSS reusam número (episódios diários/bônus/trailers)
-- ou o omitem (vira 0/NULL). A idempotência do seed já vem do DELETE+INSERT por
-- mídia, então a constraint só quebrava a ingestão sem agregar integridade real.
ALTER TABLE podcast_episodios DROP CONSTRAINT IF EXISTS podcast_episodios_midia_id_numero_key;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE podcast_episodios ADD CONSTRAINT podcast_episodios_midia_id_numero_key UNIQUE (midia_id, numero);
-- +goose StatementEnd
