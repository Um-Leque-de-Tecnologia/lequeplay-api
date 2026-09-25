package catalog

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	pgvector "github.com/pgvector/pgvector-go"

	"github.com/Um-Leque-de-Tecnologia/lequeplay-api/internal/platform/apperr"
)

// Repo é o acesso a dados do catálogo sobre Postgres (pgx).
type Repo struct {
	pool *pgxpool.Pool
}

// NewRepo constrói o repositório.
func NewRepo(pool *pgxpool.Pool) *Repo { return &Repo{pool: pool} }

// midiaColumns é a projeção comum usada para materializar uma Midia. A ordem
// das colunas é contrato com scanMidia e scanSearchItems (e com midiaColumnsM):
// mexer aqui sem mexer lá troca colunas de lugar no Scan, e o erro é mudo.
const midiaColumns = `id, slug, tipo, titulo, coalesce(titulo_original,''), coalesce(sinopse,''),
	coalesce(ano,0), generos, coalesce(poster_path,''), coalesce(duracao_min,0),
	popularidade, nota_media, total_avaliacoes, coalesce(status,''), coalesce(frequencia,'')`

// midiaColumnsM é a mesma projeção qualificada pelo alias "m" (usada no JOIN da RRF).
const midiaColumnsM = `m.id, m.slug, m.tipo, m.titulo, coalesce(m.titulo_original,''), coalesce(m.sinopse,''),
	coalesce(m.ano,0), m.generos, coalesce(m.poster_path,''), coalesce(m.duracao_min,0),
	m.popularidade, m.nota_media, m.total_avaliacoes, coalesce(m.status,''), coalesce(m.frequencia,'')`

// scanMidia lê uma linha na projeção midiaColumns.
func scanMidia(row pgx.Row) (Midia, error) {
	var (
		m      Midia
		poster string
	)
	if err := row.Scan(&m.ID, &m.Slug, &m.Tipo, &m.Titulo, &m.TituloOriginal, &m.Sinopse,
		&m.Ano, &m.Generos, &poster, &m.DuracaoMin, &m.Popularidade, &m.NotaMedia,
		&m.TotalAvaliacoes, &m.Status, &m.Frequencia); err != nil {
		return Midia{}, err
	}
	m.PosterURL = posterURL(poster)
	return m, nil
}

// filterClause monta as condições de filtro (tipo/gênero/ano) a partir de *next,
// acrescentando os argumentos. As condições são compostas com AND e podem ser
// reusadas em múltiplas CTEs (os placeholders são estáveis).
func filterClause(f Filter, next *int, args *[]any) string {
	var b strings.Builder
	if f.Tipo != "" {
		fmt.Fprintf(&b, " AND tipo = $%d", *next)
		*args = append(*args, f.Tipo)
		*next++
	}
	if f.Genero != "" {
		fmt.Fprintf(&b, " AND $%d = ANY(generos)", *next)
		*args = append(*args, f.Genero)
		*next++
	}
	if f.Ano > 0 {
		fmt.Fprintf(&b, " AND ano = $%d", *next)
		*args = append(*args, f.Ano)
		*next++
	}
	return b.String()
}

// ListMidias lista mídias com filtros, ordenadas por popularidade, paginadas.
func (r *Repo) ListMidias(ctx context.Context, f Filter) (Page[Midia], error) {
	next := 1
	var args []any
	where := filterClause(f, &next, &args)

	// A busca textual simples da listagem mora aqui, e não em filterClause, de
	// propósito: aquele trecho também monta o WHERE de /v1/busca, onde o `q` já
	// é a consulta semântica. Um ILIKE por cima restringiria o resultado
	// vetorial ao que casa por letra — matando justamente a busca que entende
	// intenção. Aqui ele existe porque a grade do catálogo manda `?q=`, e antes
	// o parâmetro era ignorado em silêncio: a tela dizia ter filtrado e a
	// resposta vinha inteira.
	if f.Q != "" {
		where += fmt.Sprintf(
			" AND (titulo ILIKE '%%' || $%d || '%%' OR coalesce(sinopse,'') ILIKE '%%' || $%d || '%%')",
			next, next,
		)
		args = append(args, f.Q)
		next++
	}

	countSQL := "SELECT count(*) FROM midias WHERE true" + where
	var total int
	if err := r.pool.QueryRow(ctx, countSQL, args...).Scan(&total); err != nil {
		return Page[Midia]{}, fmt.Errorf("count midias: %w", err)
	}

	limitPos, offsetPos := next, next+1
	listSQL := fmt.Sprintf(`SELECT %s FROM midias WHERE true%s
		ORDER BY popularidade DESC, titulo ASC
		LIMIT $%d OFFSET $%d`, midiaColumns, where, limitPos, offsetPos)
	args = append(args, f.Limite, f.Offset)

	rows, err := r.pool.Query(ctx, listSQL, args...)
	if err != nil {
		return Page[Midia]{}, fmt.Errorf("listar midias: %w", err)
	}
	defer rows.Close()

	itens := make([]Midia, 0, f.Limite)
	for rows.Next() {
		m, err := scanMidia(rows)
		if err != nil {
			return Page[Midia]{}, fmt.Errorf("scan midia: %w", err)
		}
		itens = append(itens, m)
	}
	if err := rows.Err(); err != nil {
		return Page[Midia]{}, fmt.Errorf("rows midias: %w", err)
	}
	// O envelope publica pagina/porPagina; o offset é detalhe do SQL. A divisão
	// inteira é segura porque o offset só chega aqui como múltiplo do limite
	// quando veio de `?pagina=`, e quando veio de `?offset=` cru a conta ainda
	// responde "em que página isto cairia".
	return Page[Midia]{
		Itens:     itens,
		Pagina:    f.Offset/f.Limite + 1,
		PorPagina: f.Limite,
		Total:     total,
	}, nil
}

// GetMidia retorna uma mídia com créditos e temporadas. O parâmetro aceita tanto
// o id (UUID) quanto o slug: o que tem cara de UUID é buscado por id, o resto por
// slug. Retorna NotFound quando não existe.
func (r *Repo) GetMidia(ctx context.Context, idOuSlug string) (MidiaDetalhe, error) {
	// Duas consultas constantes, uma executada: nada do parâmetro entra no SQL,
	// ele continua indo como $1.
	const (
		sqlPorID   = "SELECT " + midiaColumns + " FROM midias WHERE id = $1"
		sqlPorSlug = "SELECT " + midiaColumns + " FROM midias WHERE slug = $1"
	)
	sql := sqlPorSlug
	if isUUID(idOuSlug) {
		sql = sqlPorID
	}

	m, err := scanMidia(r.pool.QueryRow(ctx, sql, idOuSlug))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return MidiaDetalhe{}, apperr.New(apperr.KindNotFound, "Mídia não encontrada", "id ou slug inexistente")
		}
		return MidiaDetalhe{}, fmt.Errorf("buscar midia: %w", err)
	}

	detalhe := MidiaDetalhe{Midia: m}

	// Créditos e temporadas usam sempre o id da linha encontrada, nunca o
	// parâmetro recebido — que pode ser um slug.
	creditos, err := r.creditos(ctx, m.ID)
	if err != nil {
		return MidiaDetalhe{}, err
	}
	detalhe.Creditos = creditos

	switch m.Tipo {
	case "serie":
		temporadas, err := r.temporadas(ctx, m.ID)
		if err != nil {
			return MidiaDetalhe{}, err
		}
		detalhe.Temporadas = temporadas
	case "podcast":
		episodios, err := r.podcastEpisodios(ctx, m.ID)
		if err != nil {
			return MidiaDetalhe{}, err
		}
		detalhe.Episodios = episodios
	}
	return detalhe, nil
}

func (r *Repo) creditos(ctx context.Context, midiaID string) ([]Credito, error) {
	const sql = `SELECT p.nome, coalesce(p.foto_path,''), c.papel, coalesce(c.personagem,'')
		FROM creditos c JOIN pessoas p ON p.id = c.pessoa_id
		WHERE c.midia_id = $1
		ORDER BY (c.papel = 'elenco'), c.ordem, p.nome`
	rows, err := r.pool.Query(ctx, sql, midiaID)
	if err != nil {
		return nil, fmt.Errorf("creditos: %w", err)
	}
	defer rows.Close()

	var out []Credito
	for rows.Next() {
		var (
			c          Credito
			nome, foto string
		)
		if err := rows.Scan(&nome, &foto, &c.Papel, &c.Personagem); err != nil {
			return nil, fmt.Errorf("scan credito: %w", err)
		}
		// O slug da pessoa é derivado do nome, com o mesmo Slugify do slug de
		// mídia. A tabela `pessoas` não guarda slug, e derivar aqui evita uma
		// coluna que precisaria ser mantida em sincronia com o nome.
		//
		// No dia em que a pessoa ganhar página própria, o slug vira coluna: um
		// endereço publicado tem de sobreviver a uma correção de grafia no
		// nome, e derivar do nome quebraria exatamente aí.
		c.Pessoa = PessoaResumo{
			Slug:    Slugify(nome),
			Nome:    nome,
			FotoURL: posterURL(foto),
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (r *Repo) temporadas(ctx context.Context, midiaID string) ([]Temporada, error) {
	const sql = `SELECT numero, coalesce(nome,''), coalesce(ano,0), total_episodios
		FROM temporadas WHERE midia_id = $1 ORDER BY numero`
	rows, err := r.pool.Query(ctx, sql, midiaID)
	if err != nil {
		return nil, fmt.Errorf("temporadas: %w", err)
	}
	defer rows.Close()

	var out []Temporada
	for rows.Next() {
		var t Temporada
		if err := rows.Scan(&t.Numero, &t.Nome, &t.Ano, &t.TotalEpisodios); err != nil {
			return nil, fmt.Errorf("scan temporada: %w", err)
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// podcastEpisodios lista os episódios de um podcast, ordenados por número. A data
// de publicação é serializada como YYYY-MM-DD (vazia quando NULL).
func (r *Repo) podcastEpisodios(ctx context.Context, midiaID string) ([]Episodio, error) {
	const sql = `SELECT coalesce(numero,0), titulo, coalesce(duracao_min,0), publicado_em
		FROM podcast_episodios WHERE midia_id = $1
		ORDER BY publicado_em DESC NULLS LAST, numero DESC`
	rows, err := r.pool.Query(ctx, sql, midiaID)
	if err != nil {
		return nil, fmt.Errorf("podcast episodios: %w", err)
	}
	defer rows.Close()

	var out []Episodio
	for rows.Next() {
		var (
			e   Episodio
			pub *time.Time
		)
		if err := rows.Scan(&e.Numero, &e.Titulo, &e.DuracaoMin, &pub); err != nil {
			return nil, fmt.Errorf("scan episodio: %w", err)
		}
		if pub != nil {
			e.PublicadoEm = pub.Format("2006-01-02")
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// EpisodiosDaTemporada lista os episódios de uma temporada de uma série. A mídia
// é referenciada por UUID ou slug e a temporada pelo número. Retorna NotFound
// quando a mídia ou a temporada não existem.
func (r *Repo) EpisodiosDaTemporada(ctx context.Context, idOuSlug string, numero int) ([]EpisodioTemporada, error) {
	column := "slug"
	if isUUID(idOuSlug) {
		column = "id"
	}

	var temporadaID string
	sql := `SELECT t.id FROM temporadas t
		JOIN midias m ON m.id = t.midia_id
		WHERE m.` + column + ` = $1 AND t.numero = $2`
	if err := r.pool.QueryRow(ctx, sql, idOuSlug, numero).Scan(&temporadaID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apperr.New(apperr.KindNotFound, "Temporada não encontrada", "mídia ou temporada inexistente")
		}
		return nil, fmt.Errorf("buscar temporada: %w", err)
	}

	const epSQL = `SELECT numero, coalesce(nome,''), coalesce(sinopse,''), coalesce(duracao_min,0)
		FROM episodios WHERE temporada_id = $1 ORDER BY numero`
	rows, err := r.pool.Query(ctx, epSQL, temporadaID)
	if err != nil {
		return nil, fmt.Errorf("episodios da temporada: %w", err)
	}
	defer rows.Close()

	out := make([]EpisodioTemporada, 0)
	for rows.Next() {
		var e EpisodioTemporada
		if err := rows.Scan(&e.Numero, &e.Titulo, &e.Sinopse, &e.DuracaoMin); err != nil {
			return nil, fmt.Errorf("scan episodio temporada: %w", err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// ListGeneros lista os nomes dos gêneros do catálogo em ordem alfabética.
//
// Nomes, e não {id, nome}: o filtro publicado é `?genero=Drama`, então o que o
// cliente faz com esta lista é devolver o próprio valor na query. O id da
// tabela não tem uso publicado, e expor identificador que ninguém consome cria
// contrato para manter sem ninguém ganhar nada.
func (r *Repo) ListGeneros(ctx context.Context) ([]string, error) {
	rows, err := r.pool.Query(ctx, "SELECT nome FROM generos ORDER BY nome")
	if err != nil {
		return nil, fmt.Errorf("listar generos: %w", err)
	}
	defer rows.Close()

	// Slice não-nula desde o começo: um catálogo sem gêneros é `itens: []`.
	// Com `var out []string`, o JSON sairia `null` e obrigaria todo cliente a
	// tratar um terceiro estado que não existe no domínio.
	out := []string{}
	for rows.Next() {
		var nome string
		if err := rows.Scan(&nome); err != nil {
			return nil, fmt.Errorf("scan genero: %w", err)
		}
		out = append(out, nome)
	}
	return out, rows.Err()
}

// CatalogVersion retorna a versão atual do catálogo (para invalidação de cache).
func (r *Repo) CatalogVersion(ctx context.Context) (int64, error) {
	var v int64
	if err := r.pool.QueryRow(ctx, "SELECT versao FROM catalog_version WHERE id = true").Scan(&v); err != nil {
		return 0, fmt.Errorf("catalog version: %w", err)
	}
	return v, nil
}

// scanSearchItems materializa itens de busca (projeção midiaColumns + score).
func scanSearchItems(rows pgx.Rows) ([]SearchItem, error) {
	defer rows.Close()
	var items []SearchItem
	for rows.Next() {
		var (
			it     SearchItem
			poster string
		)
		if err := rows.Scan(&it.ID, &it.Slug, &it.Tipo, &it.Titulo, &it.TituloOriginal, &it.Sinopse,
			&it.Ano, &it.Generos, &poster, &it.DuracaoMin, &it.Popularidade, &it.NotaMedia,
			&it.TotalAvaliacoes, &it.Status, &it.Frequencia,
			&it.Score); err != nil {
			return nil, fmt.Errorf("scan search item: %w", err)
		}
		it.PosterURL = posterURL(poster)
		items = append(items, it)
	}
	return items, rows.Err()
}

// FTS roda a busca full-text (português) + fuzzy por título (trigram), ordenada
// por relevância lexical. É a perna léxica da busca híbrida.
func (r *Repo) FTS(ctx context.Context, query string, f Filter, limit int) ([]SearchItem, error) {
	args := []any{query}
	next := 2
	limitPos := next
	args = append(args, limit)
	next++
	where := filterClause(f, &next, &args)

	sql := fmt.Sprintf(`
SELECT %s,
       ts_rank_cd(fts, websearch_to_tsquery('portuguese', $1)) AS score
FROM midias
WHERE (fts @@ websearch_to_tsquery('portuguese', $1) OR titulo %% $1)%s
ORDER BY score DESC, similarity(titulo, $1) DESC
LIMIT $%d`, midiaColumns, where, limitPos)

	rows, err := r.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("fts query: %w", err)
	}
	return scanSearchItems(rows)
}

// Vector roda a busca KNN por cosseno sobre o embedding, retornando a similaridade
// (1 - distância) como score (maior = melhor).
func (r *Repo) Vector(ctx context.Context, qvec []float32, f Filter, limit int) ([]SearchItem, error) {
	args := []any{pgvector.NewVector(qvec)}
	next := 2
	limitPos := next
	args = append(args, limit)
	next++
	where := filterClause(f, &next, &args)

	sql := fmt.Sprintf(`
SELECT %s,
       1 - (embedding <=> $1::vector) AS score
FROM midias
WHERE embedding IS NOT NULL%s
ORDER BY embedding <=> $1::vector
LIMIT $%d`, midiaColumns, where, limitPos)

	rows, err := r.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("vector query: %w", err)
	}
	return scanSearchItems(rows)
}

// HybridRRF funde a perna léxica e a semântica com Reciprocal Rank Fusion (k=60)
// em SQL, retornando os `limit` melhores candidatos. Cada perna contribui com
// seus top `retrieveN`; o score fundido é SUM(1/(60+rank)).
func (r *Repo) HybridRRF(ctx context.Context, query string, qvec []float32, f Filter, retrieveN, limit int) ([]SearchItem, error) {
	// $1 = query, $2 = vetor, $3 = cap por perna, $4 = cap de saída.
	args := []any{query, pgvector.NewVector(qvec), retrieveN, limit}
	next := 5
	// O mesmo conjunto de filtros é referenciado nas duas CTEs.
	where := filterClause(f, &next, &args)

	sql := fmt.Sprintf(`
WITH lexical AS (
  SELECT id, ROW_NUMBER() OVER (ORDER BY ts_rank_cd(fts, websearch_to_tsquery('portuguese', $1)) DESC) AS rk
  FROM midias
  WHERE (fts @@ websearch_to_tsquery('portuguese', $1) OR titulo %% $1)%[2]s
  ORDER BY ts_rank_cd(fts, websearch_to_tsquery('portuguese', $1)) DESC
  LIMIT $3
),
semantic AS (
  SELECT id, ROW_NUMBER() OVER (ORDER BY embedding <=> $2::vector) AS rk
  FROM midias
  WHERE embedding IS NOT NULL%[2]s
  ORDER BY embedding <=> $2::vector
  LIMIT $3
),
rrf AS (
  SELECT id, 1.0/(60+rk) AS s FROM lexical
  UNION ALL
  SELECT id, 1.0/(60+rk) AS s FROM semantic
)
SELECT %[1]s, SUM(rrf.s) AS score
FROM rrf JOIN midias m ON m.id = rrf.id
GROUP BY m.id
ORDER BY score DESC
LIMIT $4`, midiaColumnsM, where)

	rows, err := r.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("hybrid rrf query: %w", err)
	}
	return scanSearchItems(rows)
}

// isUUID valida o formato canônico 8-4-4-4-12.
func isUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i, c := range s {
		switch i {
		case 8, 13, 18, 23:
			if c != '-' {
				return false
			}
		default:
			if (c < '0' || c > '9') && (c < 'a' || c > 'f') && (c < 'A' || c > 'F') {
				return false
			}
		}
	}
	return true
}
