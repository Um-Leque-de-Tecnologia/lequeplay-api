-- +goose Up
-- +goose StatementBegin

-- Duas informações que o front consome e que a API não publicava.
--
-- total_avaliacoes: quantas pessoas avaliaram o título. Sem ela, nota_media
-- carrega duas perguntas ao mesmo tempo — "qual é a nota?" e "existe nota?" —
-- e o zero vira resposta ambígua: um título que ninguém avaliou fica
-- indistinguível de um que todo mundo detestou. Com o contador ao lado, a
-- segunda pergunta ganha dono próprio e a nota volta a ser só um número.
--
-- NOT NULL DEFAULT 0 de propósito: ausência de avaliação É zero avaliações,
-- não é desconhecido. Nullable aqui obrigaria todo consumidor a tratar um
-- terceiro estado que não existe no domínio.
ALTER TABLE midias ADD COLUMN total_avaliacoes integer NOT NULL DEFAULT 0;

-- status: a situação de produção do título, como a TMDB publica — "Returning
-- Series", "Ended", "Canceled" para série; "Released" para filme.
--
-- NULLABLE, ao contrário do contador: aqui a ausência é real. Podcast semeado
-- à mão não tem status, e a coluna guarda o texto da origem sem traduzir. O
-- dia em que o produto quiser rótulo em português, a tradução é decisão de
-- apresentação e mora no front — o banco guarda o dado, não a legenda.
ALTER TABLE midias ADD COLUMN status text;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE midias DROP COLUMN IF EXISTS status;
ALTER TABLE midias DROP COLUMN IF EXISTS total_avaliacoes;
-- +goose StatementEnd
