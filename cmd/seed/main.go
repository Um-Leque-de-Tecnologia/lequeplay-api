// Command seed popula o catálogo: busca filmes e séries populares na TMDB
// (pt-BR), gera os embeddings via Gemini (RETRIEVAL_DOCUMENT) e carrega tudo no
// Postgres, incrementando a versão do catálogo ao final.
//
// Quantidades configuráveis por env: SEED_MOVIES (default 40), SEED_SERIES (20).
// Idempotente: reexecutar faz upsert por (tipo, tmdb_id).
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	pgvector "github.com/pgvector/pgvector-go"

	migrations "github.com/Um-Leque-de-Tecnologia/lequeplay-api/db/migrations"
	"github.com/Um-Leque-de-Tecnologia/lequeplay-api/internal/integrations/gemini"
	"github.com/Um-Leque-de-Tecnologia/lequeplay-api/internal/integrations/tmdb"
	"github.com/Um-Leque-de-Tecnologia/lequeplay-api/internal/platform/config"
	"github.com/Um-Leque-de-Tecnologia/lequeplay-api/internal/platform/postgres"
)

func main() {
	if err := run(); err != nil {
		slog.Error("seed falhou", slog.String("err", err.Error()))
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	if cfg.TMDB.ReadToken == "" {
		return fmt.Errorf("TMDB_READ_TOKEN é obrigatório para o seed")
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Garante o schema antes de inserir.
	if err := postgres.Migrate(ctx, cfg.DB.DSN, migrations.FS); err != nil {
		return err
	}
	pool, err := postgres.NewPool(ctx, cfg.DB)
	if err != nil {
		return err
	}
	defer pool.Close()

	nMovies := envInt("SEED_MOVIES", 40)
	nSeries := envInt("SEED_SERIES", 20)

	tm := tmdb.New(cfg.TMDB.ReadToken)
	logger.Info("buscando catálogo na TMDB", "filmes", nMovies, "series", nSeries)

	movies, err := tm.FetchPopular(ctx, tmdb.KindMovie, nMovies)
	if err != nil {
		return fmt.Errorf("buscar filmes: %w", err)
	}
	series, err := tm.FetchPopular(ctx, tmdb.KindSeries, nSeries)
	if err != nil {
		return fmt.Errorf("buscar séries: %w", err)
	}
	titles := append(movies, series...)
	logger.Info("títulos obtidos", "total", len(titles))

	// Embeddings via Gemini (RETRIEVAL_DOCUMENT). Sem key, segue sem vetor.
	gem := gemini.New(cfg.Gemini.APIKey)
	vectors := make([][]float32, len(titles))
	if gem.Enabled() {
		if err := embedTitles(ctx, logger, gem, titles, vectors); err != nil {
			return err
		}
	} else {
		logger.Warn("GEMINI_API_KEY vazia: mídias serão inseridas sem embedding (busca semântica desabilitada)")
	}

	inserted, err := load(ctx, pool, titles, vectors)
	if err != nil {
		return err
	}
	logger.Info("seed concluído", "midias", inserted)
	return nil
}

// embedTitles preenche `vectors` com o embedding de cada título, em lotes com
// backoff exponencial. Um lote que falhe após as tentativas aborta o seed.
func embedTitles(ctx context.Context, logger *slog.Logger, gem *gemini.Client, titles []tmdb.Title, vectors [][]float32) error {
	texts := make([]string, len(titles))
	for i, t := range titles {
		texts[i] = docText(t)
	}

	for start := 0; start < len(texts); start += gemini.MaxBatch {
		end := start + gemini.MaxBatch
		if end > len(texts) {
			end = len(texts)
		}

		var (
			vecs [][]float32
			err  error
		)
		for attempt := 0; attempt < 6; attempt++ {
			vecs, err = gem.EmbedDocumentsBatch(ctx, texts[start:end])
			if err == nil {
				break
			}
			wait := time.Duration(2<<attempt) * time.Second
			if wait > 30*time.Second {
				wait = 30 * time.Second
			}
			logger.Warn("lote de embedding falhou", "faixa", fmt.Sprintf("%d-%d", start, end), "tentativa", attempt+1, "erro", err, "retry_em", wait.String())
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(wait):
			}
		}
		if err != nil {
			return fmt.Errorf("embedding do lote %d-%d: %w", start, end, err)
		}
		copy(vectors[start:end], vecs)
		logger.Info("lote embutido", "faixa", fmt.Sprintf("%d-%d", start, end))
	}
	return nil
}

// docText monta o texto do documento a ser embutido: título, sinopse, gêneros,
// elenco e direção — o que dá significado semântico ao título.
func docText(t tmdb.Title) string {
	var elenco, direcao []string
	for _, p := range t.Creditos {
		switch p.Papel {
		case "elenco":
			elenco = append(elenco, p.Nome)
		case "direcao":
			direcao = append(direcao, p.Nome)
		}
	}
	parts := []string{t.Titulo}
	if t.TituloOrig != "" && t.TituloOrig != t.Titulo {
		parts = append(parts, t.TituloOrig)
	}
	if t.Sinopse != "" {
		parts = append(parts, t.Sinopse)
	}
	if len(t.Generos) > 0 {
		parts = append(parts, "Gêneros: "+strings.Join(t.Generos, ", "))
	}
	if len(direcao) > 0 {
		parts = append(parts, "Direção: "+strings.Join(direcao, ", "))
	}
	if len(elenco) > 0 {
		parts = append(parts, "Elenco: "+strings.Join(elenco, ", "))
	}
	return strings.Join(parts, ". ")
}

// load insere todos os títulos (e seus créditos/temporadas) numa transação e
// incrementa a versão do catálogo.
func load(ctx context.Context, pool *pgxpool.Pool, titles []tmdb.Title, vectors [][]float32) (int, error) {
	var count int
	err := postgres.InTx(ctx, pool, func(tx pgx.Tx) error {
		for i, t := range titles {
			if err := upsertTitle(ctx, tx, t, vectors[i]); err != nil {
				return fmt.Errorf("upsert %q: %w", t.Titulo, err)
			}
			count++
		}
		_, err := tx.Exec(ctx, "UPDATE catalog_version SET versao = versao + 1, updated_at = now() WHERE id = true")
		return err
	})
	return count, err
}

func upsertTitle(ctx context.Context, tx pgx.Tx, t tmdb.Title, vec []float32) error {
	// Gêneros (tabela de navegação).
	for _, g := range t.Generos {
		if _, err := tx.Exec(ctx, "INSERT INTO generos (nome) VALUES ($1) ON CONFLICT (nome) DO NOTHING", g); err != nil {
			return fmt.Errorf("genero: %w", err)
		}
	}

	var embedding any
	if len(vec) > 0 {
		embedding = pgvector.NewVector(vec)
	}

	var midiaID string
	err := tx.QueryRow(ctx, `
INSERT INTO midias (tipo, tmdb_id, titulo, titulo_original, sinopse, ano, generos,
	poster_path, duracao_min, popularidade, nota_media, embedding, updated_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12, now())
ON CONFLICT (tipo, tmdb_id) DO UPDATE SET
	titulo = EXCLUDED.titulo,
	titulo_original = EXCLUDED.titulo_original,
	sinopse = EXCLUDED.sinopse,
	ano = EXCLUDED.ano,
	generos = EXCLUDED.generos,
	poster_path = EXCLUDED.poster_path,
	duracao_min = EXCLUDED.duracao_min,
	popularidade = EXCLUDED.popularidade,
	nota_media = EXCLUDED.nota_media,
	embedding = COALESCE(EXCLUDED.embedding, midias.embedding),
	updated_at = now()
RETURNING id`,
		string(t.Tipo), nullInt(t.TMDBID), t.Titulo, nullStr(t.TituloOrig), nullStr(t.Sinopse),
		nullInt(t.Ano), t.Generos, nullStr(t.PosterPath), nullInt(t.DuracaoMin),
		t.Popularidade, t.NotaMedia, embedding).Scan(&midiaID)
	if err != nil {
		return fmt.Errorf("midia: %w", err)
	}

	// Recria créditos e temporadas do zero para manter idempotência.
	if _, err := tx.Exec(ctx, "DELETE FROM creditos WHERE midia_id = $1", midiaID); err != nil {
		return fmt.Errorf("limpar creditos: %w", err)
	}
	for _, p := range t.Creditos {
		var pessoaID string
		err := tx.QueryRow(ctx, `
INSERT INTO pessoas (tmdb_id, nome, foto_path) VALUES ($1,$2,$3)
ON CONFLICT (tmdb_id) DO UPDATE SET nome = EXCLUDED.nome, foto_path = EXCLUDED.foto_path
RETURNING id`, nullInt(p.TMDBID), p.Nome, nullStr(p.FotoPath)).Scan(&pessoaID)
		if err != nil {
			return fmt.Errorf("pessoa %q: %w", p.Nome, err)
		}
		if _, err := tx.Exec(ctx, `
INSERT INTO creditos (midia_id, pessoa_id, papel, personagem, ordem)
VALUES ($1,$2,$3,$4,$5)
ON CONFLICT (midia_id, pessoa_id, papel, personagem) DO NOTHING`,
			midiaID, pessoaID, p.Papel, nullStr(p.Personagem), p.Ordem); err != nil {
			return fmt.Errorf("credito: %w", err)
		}
	}

	if _, err := tx.Exec(ctx, "DELETE FROM temporadas WHERE midia_id = $1", midiaID); err != nil {
		return fmt.Errorf("limpar temporadas: %w", err)
	}
	for _, s := range t.Temporadas {
		if _, err := tx.Exec(ctx, `
INSERT INTO temporadas (midia_id, numero, nome, ano, total_episodios)
VALUES ($1,$2,$3,$4,$5)
ON CONFLICT (midia_id, numero) DO UPDATE SET
	nome = EXCLUDED.nome, ano = EXCLUDED.ano, total_episodios = EXCLUDED.total_episodios`,
			midiaID, s.Numero, nullStr(s.Nome), nullInt(s.Ano), s.TotalEpisodios); err != nil {
			return fmt.Errorf("temporada: %w", err)
		}
	}
	return nil
}

func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return def
}

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nullInt(n int) any {
	if n == 0 {
		return nil
	}
	return n
}
