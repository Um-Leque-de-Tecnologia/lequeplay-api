// Package catalog contém o domínio do catálogo público (mídias, pessoas,
// créditos, gêneros) e a busca sobre ele.
package catalog

import "strings"

// posterBase é o prefixo do CDN de imagens da TMDB (tamanho w342).
const posterBase = "https://image.tmdb.org/t/p/w342"

// posterURL monta a URL pública do pôster a partir do path da TMDB. Quando o valor
// já é uma URL absoluta (ex.: capa de podcast vinda da Apple), é devolvido como está.
func posterURL(path string) string {
	if path == "" {
		return ""
	}
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		return path
	}
	return posterBase + path
}

// Midia é um item do catálogo (filme, série ou podcast).
type Midia struct {
	ID string `json:"id"`
	// Slug é o identificador legível e estável do título, aceito no lugar do id
	// na rota de detalhe. Sem `omitempty` de propósito: slug vazio é defeito, e
	// sumir do JSON apenas esconderia o defeito de quem consome.
	Slug           string   `json:"slug"`
	Tipo           string   `json:"tipo"`
	Titulo         string   `json:"titulo"`
	TituloOriginal string   `json:"tituloOriginal,omitempty"`
	Sinopse        string   `json:"sinopse,omitempty"`
	Ano            int      `json:"ano,omitempty"`
	Generos        []string `json:"generos"`
	PosterURL      string   `json:"posterUrl,omitempty"`
	DuracaoMin     int      `json:"duracaoMin,omitempty"`
	Popularidade   float32  `json:"popularidade"`
	NotaMedia      float32  `json:"notaMedia"`
	// TotalAvaliacoes é quantas pessoas avaliaram o título.
	//
	// Sem `omitempty`, e é o ponto todo do campo: `notaMedia` sozinha não
	// distingue "ninguém avaliou" (nota 0 por ausência) de "todo mundo
	// detestou" (nota 0 de verdade). Quem responde "tem nota?" é o contador, e
	// um contador que some do JSON quando vale zero devolveria a ambiguidade
	// que ele existe para desfazer.
	TotalAvaliacoes int `json:"totalAvaliacoes"`
	// Status é a situação de produção do título: "Returning Series", "Ended",
	// "Canceled" para séries, "Released" para filmes. Vem da TMDB como está.
	//
	// Com `omitempty` porque nem todo título tem: o catálogo aceita podcast, e
	// um título semeado à mão pode não trazer nada. Campo ausente é melhor que
	// string vazia — quem consome checa a existência, não o conteúdo.
	Status string `json:"status,omitempty"`
	// Frequencia é a periodicidade de um podcast (ex.: "Semanal"). Com `omitempty`
	// porque só faz sentido para tipo=podcast; filme e série a omitem.
	Frequencia string `json:"frequencia,omitempty"`
}

// PessoaResumo identifica quem assina um crédito.
//
// Objeto, e não o nome solto, porque nome não é identificador: existem dois
// "João Silva", e a tela da pessoa precisa de um endereço estável. O slug sai
// do mesmo Slugify que gera o slug da mídia.
type PessoaResumo struct {
	Slug    string `json:"slug"`
	Nome    string `json:"nome"`
	FotoURL string `json:"fotoUrl,omitempty"`
}

// Credito liga uma pessoa a um título com um papel.
type Credito struct {
	Pessoa     PessoaResumo `json:"pessoa"`
	Papel      string       `json:"papel"`
	Personagem string       `json:"personagem,omitempty"`
}

// Temporada é uma temporada de uma série.
type Temporada struct {
	Numero         int    `json:"numero"`
	Nome           string `json:"nome,omitempty"`
	Ano            int    `json:"ano,omitempty"`
	TotalEpisodios int    `json:"totalEpisodios"`
}

// Episodio é um episódio de podcast, ligado direto à mídia (sem temporada).
type Episodio struct {
	Numero      int    `json:"numero,omitempty"`
	Titulo      string `json:"titulo"`
	DuracaoMin  int    `json:"duracaoMin,omitempty"`
	PublicadoEm string `json:"publicadoEm,omitempty"` // ISO YYYY-MM-DD
}

// EpisodioTemporada é um episódio de uma temporada de série.
type EpisodioTemporada struct {
	Numero     int    `json:"numero"`
	Titulo     string `json:"titulo,omitempty"`
	DuracaoMin int    `json:"duracaoMin,omitempty"`
	Sinopse    string `json:"sinopse,omitempty"`
}

// MidiaDetalhe é a mídia com seus créditos, temporadas (séries) e episódios (podcasts).
type MidiaDetalhe struct {
	Midia
	Creditos   []Credito   `json:"creditos"`
	Temporadas []Temporada `json:"temporadas,omitempty"`
	Episodios  []Episodio  `json:"episodios,omitempty"`
}

// SearchItem é um resultado de busca: a mídia mais o score e a posição no ranking.
type SearchItem struct {
	Midia
	Score float64 `json:"score"`
	Rank  int     `json:"rank"`
}

// Page é um envelope de paginação padronizado.
//
// Publica `pagina` e `porPagina`, e não `limite`/`offset`: é assim que o
// contrato com o front está escrito, e é o que a tela precisa para montar
// "página 2 de 6". Dentro do repositório a conta continua sendo limite e
// offset, que é o que o SQL entende — a tradução acontece num lugar só, ao
// montar esta resposta.
type Page[T any] struct {
	Itens     []T `json:"itens"`
	Pagina    int `json:"pagina"`
	PorPagina int `json:"porPagina"`
	Total     int `json:"total"`
}

// Filter agrega os filtros de listagem/busca do catálogo.
type Filter struct {
	Tipo   string
	Genero string
	Ano    int
	// Q é a busca textual simples da listagem — casa com título e sinopse.
	// Não confundir com /v1/busca, que é a busca de verdade, com vetor e RRF:
	// este aqui existe porque o catálogo manda `?q=` ao filtrar a grade.
	Q      string
	Limite int
	Offset int
}

// rerankText é o texto de um item entregue ao reranker: título + sinopse.
func rerankText(it SearchItem) string {
	if it.Sinopse == "" {
		return it.Titulo
	}
	return strings.TrimSpace(it.Titulo + ". " + it.Sinopse)
}
