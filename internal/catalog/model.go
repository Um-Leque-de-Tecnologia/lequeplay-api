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
	ID             string   `json:"id"`
	Slug           string   `json:"slug,omitempty"`
	Tipo           string   `json:"tipo"`
	Titulo         string   `json:"titulo"`
	TituloOriginal string   `json:"tituloOriginal,omitempty"`
	Sinopse        string   `json:"sinopse,omitempty"`
	Ano            int      `json:"ano,omitempty"`
	Generos        []string `json:"generos"`
	PosterURL      string   `json:"posterUrl,omitempty"`
	DuracaoMin     int      `json:"duracaoMin,omitempty"`
	// Frequencia é a periodicidade de um podcast (ex.: "Semanal"). Vazio para
	// filmes e séries.
	Frequencia   string  `json:"frequencia,omitempty"`
	Popularidade float32 `json:"popularidade"`
	NotaMedia    float32 `json:"notaMedia"`
}

// Credito liga uma pessoa a um título com um papel.
type Credito struct {
	Pessoa     string `json:"pessoa"`
	FotoURL    string `json:"fotoUrl,omitempty"`
	Papel      string `json:"papel"`
	Personagem string `json:"personagem,omitempty"`
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

// Genero é um gênero do catálogo.
type Genero struct {
	ID   int    `json:"id"`
	Nome string `json:"nome"`
}

// SearchItem é um resultado de busca: a mídia mais o score e a posição no ranking.
type SearchItem struct {
	Midia
	Score float64 `json:"score"`
	Rank  int     `json:"rank"`
}

// Page é um envelope de paginação padronizado.
type Page[T any] struct {
	Itens  []T `json:"itens"`
	Total  int `json:"total"`
	Limite int `json:"limite"`
	Offset int `json:"offset"`
}

// Filter agrega os filtros de listagem/busca do catálogo.
type Filter struct {
	Tipo   string
	Genero string
	Ano    int
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
