// Package catalog contém o domínio do catálogo público (mídias, pessoas,
// créditos, gêneros) e a busca sobre ele.
package catalog

import "strings"

// posterBase é o prefixo do CDN de imagens da TMDB (tamanho w342).
const posterBase = "https://image.tmdb.org/t/p/w342"

// posterURL monta a URL pública do pôster a partir do path da TMDB.
func posterURL(path string) string {
	if path == "" {
		return ""
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

// MidiaDetalhe é a mídia com seus créditos e temporadas.
type MidiaDetalhe struct {
	Midia
	Creditos   []Credito   `json:"creditos"`
	Temporadas []Temporada `json:"temporadas,omitempty"`
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
