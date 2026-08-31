-- +goose Up
-- +goose StatementBegin

-- Slug público das mídias: a chave estável que aparece na URL.
--
-- ATENÇÃO: este backfill espelha, em SQL, a mesma regra de internal/catalog/slug.go.
-- Os dois lados precisam produzir exatamente o mesmo slug para o mesmo título.
-- Mudar um sem mudar o outro parte os links: o seed passa a gravar um slug
-- diferente do que esta migration gerou, e a URL publicada ontem deixa de resolver.
-- Qualquer ajuste na regra vale para os dois arquivos, no mesmo commit.
--
-- A regra, idêntica dos dois lados:
--   1. minúsculas;
--   2. acentos viram a letra base, pelo mapa fixo do translate() abaixo;
--   3. tudo que não for [a-z0-9] vira hifen;
--   4. hifens repetidos viram um só; hifen das pontas é removido;
--   5. se sobrar string vazia, o slug é "sem-titulo".
--
-- Três detalhes de dialeto, todos deliberados:
--
--   * Sem unaccent. A extensão pode não estar instalada no banco, e uma migration
--     que depende de extensão opcional quebra em ambiente limpo. O mapa de acentos
--     é explícito no translate().
--   * O translate() vem ANTES do lower(), e o mapa cobre também as maiúsculas
--     acentuadas. Num banco com LC_CTYPE=C o lower() não dobra acento, e "Ódio"
--     viraria "dio" em vez de "odio"; traduzindo primeiro, o resultado independe
--     do locale e casa com o strings.ToLower do Go.
--   * A classe de caracteres é enumerada em vez de [a-z0-9]. Faixa dentro de
--     bracket expression depende do collation; enumerada, ela casa com o teste por
--     code point do lado Go em qualquer banco.

ALTER TABLE midias ADD COLUMN slug text;

-- Backfill das linhas que já existem. A coluna fts é GENERATED e não é tocada aqui.
--
-- Desambiguação, na ordem: base -> base-<ano> -> base-<ano>-<tmdb_id>. Vence o
-- primeiro candidato livre. Quando o ano é 0/ausente, o candidato do meio é pulado
-- (base -> base-<tmdb_id>).
--
-- POR QUE ano e tmdb_id, e não base-2 / base-3: sufixo por ordem de inserção muda a
-- cada re-seed. A ordem em que a TMDB devolve os títulos não é estável, então o
-- título que ontem ficou com "base-2" hoje pode ficar com "base-3", e o link antigo
-- passa a apontar para outro filme. Ano e tmdb_id são propriedades do próprio
-- título: o slug sai igual toda vez que o seed roda.
--
-- CONTRATO com o lado Go, que também vale para o Go: quando uma base repete,
-- NENHUMA das linhas em conflito fica com a base pura -- todas descem juntas para o
-- próximo candidato. Deixar a "primeira" ficar com a base pura reintroduziria
-- justamente a dependência de ordem de inserção que a regra existe para evitar.
-- Por isso o Go precisa enxergar o lote inteiro de títulos, não decidir linha a linha.
WITH bases AS (
    SELECT
        m.id,
        m.ano,
        m.tmdb_id,
        COALESCE(
            NULLIF(
                regexp_replace(
                    regexp_replace(
                        lower(translate(
                            COALESCE(m.titulo, ''),
                            'áàâãäéèêëíìîïóòôõöúùûüçñÁÀÂÃÄÉÈÊËÍÌÎÏÓÒÔÕÖÚÙÛÜÇÑ',
                            'aaaaaeeeeiiiiooooouuuucnaaaaaeeeeiiiiooooouuuucn')),
                        '[^abcdefghijklmnopqrstuvwxyz0123456789]+', '-', 'g'),
                    '^-+|-+$', '', 'g'),
                ''),
            'sem-titulo') AS base
    FROM midias m
),
candidatos AS (
    SELECT
        b.id,
        b.base,
        CASE WHEN COALESCE(b.ano, 0) <> 0
             THEN b.base || '-' || b.ano::text
        END AS cand_ano,
        CASE
            WHEN b.tmdb_id IS NULL THEN NULL
            WHEN COALESCE(b.ano, 0) <> 0
                THEN b.base || '-' || b.ano::text || '-' || b.tmdb_id::text
            ELSE b.base || '-' || b.tmdb_id::text
        END AS cand_tmdb,
        b.base || '-' || b.id::text AS cand_id
    FROM bases b
),
-- Nível 1: base sozinha no catálogo.
n1 AS (
    SELECT c.*, count(*) OVER (PARTITION BY c.base) AS repete_base
    FROM candidatos c
),
nivel1 AS (
    SELECT id, base AS slug FROM n1 WHERE repete_base = 1
),
resto1 AS (
    SELECT * FROM n1 WHERE repete_base > 1
),
-- Nível 2: base-<ano>, se o ano existir, não repetir entre os empatados e não
-- colidir com um slug já entregue no nível 1 (um título pode se chamar,
-- literalmente, "Homem Aranha 2021").
n2 AS (
    SELECT r.*, count(*) OVER (PARTITION BY r.cand_ano) AS repete_ano
    FROM resto1 r
),
nivel2 AS (
    SELECT n2.id, n2.cand_ano AS slug
    FROM n2
    WHERE n2.cand_ano IS NOT NULL
      AND n2.repete_ano = 1
      AND NOT EXISTS (SELECT 1 FROM nivel1 WHERE nivel1.slug = n2.cand_ano)
),
resto2 AS (
    SELECT * FROM n2 WHERE NOT EXISTS (SELECT 1 FROM nivel2 WHERE nivel2.id = n2.id)
),
-- Nível 3: base-<ano>-<tmdb_id> (ou base-<tmdb_id> sem ano).
n3 AS (
    SELECT r.*, count(*) OVER (PARTITION BY r.cand_tmdb) AS repete_tmdb
    FROM resto2 r
),
nivel3 AS (
    SELECT n3.id, n3.cand_tmdb AS slug
    FROM n3
    WHERE n3.cand_tmdb IS NOT NULL
      AND n3.repete_tmdb = 1
      AND NOT EXISTS (SELECT 1 FROM nivel1 WHERE nivel1.slug = n3.cand_tmdb)
      AND NOT EXISTS (SELECT 1 FROM nivel2 WHERE nivel2.slug = n3.cand_tmdb)
),
-- Rede de segurança. tmdb_id é NULLABLE, e a unicidade da tabela é (tipo, tmdb_id):
-- duas linhas sem tmdb_id, ou um filme e uma série de mesmo tmdb_id, chegam aqui com
-- os três candidatos empatados. O id da linha é único por construção, então o índice
-- único nunca aborta a migration. Não acontece com dado vindo do seed; existe para a
-- migration não falhar num banco real. É terminal de propósito: não filtra nada, ou
-- sobrariam linhas com slug NULL e o SET NOT NULL lá embaixo quebraria.
nivel4 AS (
    SELECT n3.id, n3.cand_id AS slug
    FROM n3
    WHERE NOT EXISTS (SELECT 1 FROM nivel3 WHERE nivel3.id = n3.id)
),
finais AS (
    SELECT * FROM nivel1
    UNION ALL SELECT * FROM nivel2
    UNION ALL SELECT * FROM nivel3
    UNION ALL SELECT * FROM nivel4
)
UPDATE midias m
SET slug = f.slug
FROM finais f
WHERE m.id = f.id;

-- Índice único e NOT NULL só DEPOIS do backfill: a coluna nasce NULL em todas as
-- linhas, então inverter esta ordem faria a migration falhar em qualquer banco que
-- já tenha catálogo.
CREATE UNIQUE INDEX midias_slug_uniq ON midias (slug);
ALTER TABLE midias ALTER COLUMN slug SET NOT NULL;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS midias_slug_uniq;
ALTER TABLE midias DROP COLUMN IF EXISTS slug;
-- +goose StatementEnd
