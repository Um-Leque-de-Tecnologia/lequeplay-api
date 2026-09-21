// Package tmdb é o cliente da API do The Movie Database, usado pelo pipeline de
// seed para buscar filmes e séries populares (pt-BR) com créditos.
//
// Atribuição: este produto usa a API do TMDB mas não é endossado nem certificado
// pelo TMDB.
package tmdb

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	baseURL = "https://api.themoviedb.org/3"

	resultsPerPage  = 20
	maxDiscoverPage = 500
	detailWorkers   = 24
	discoverWorkers = 12
	maxRetries      = 5
	topCast         = 8
)

// Kind é o tipo de mídia buscado na TMDB.
type Kind string

const (
	// KindMovie mapeia para o tipo de domínio "filme".
	KindMovie Kind = "filme"
	// KindSeries mapeia para o tipo de domínio "serie".
	KindSeries Kind = "serie"
)

// Person é um crédito (elenco ou equipe) associado a um título.
type Person struct {
	TMDBID     int
	Nome       string
	FotoPath   string
	Papel      string // "direcao" | "elenco" | "apresentacao"
	Personagem string
	Ordem      int
}

// Season é uma temporada de uma série.
type Season struct {
	Numero         int
	Nome           string
	Ano            int
	TotalEpisodios int
}

// Title é um título do catálogo (filme ou série) já mapeado para o domínio.
type Title struct {
	Tipo       Kind
	TMDBID     int
	Titulo     string
	TituloOrig string
	Sinopse    string
	Ano        int
	Generos    []string
	PosterPath string
	DuracaoMin int
	// Popularidade e NotaMedia vêm da TMDB; TotalAvaliacoes é o vote_count, e
	// os dois últimos andam juntos: a média sem a contagem não diz se alguém
	// avaliou.
	Popularidade    float32
	NotaMedia       float32
	TotalAvaliacoes int
	// Status é o texto de produção da TMDB ("Ended", "Returning Series",
	// "Released"), guardado como vem. Traduzir é decisão de tela.
	Status     string
	Creditos   []Person
	Temporadas []Season
}

// Client chama a API TMDB v3 usando um token de leitura v4 (Bearer).
type Client struct {
	Token string
	HTTP  *http.Client
}

// New constrói um cliente TMDB.
func New(token string) *Client {
	return &Client{Token: token, HTTP: &http.Client{Timeout: 20 * time.Second}}
}

// --- wire types ---

type discoverResponse struct {
	Results []struct {
		ID int `json:"id"`
	} `json:"results"`
}

type credits struct {
	Cast []struct {
		ID         int    `json:"id"`
		Name       string `json:"name"`
		Character  string `json:"character"`
		ProfileURL string `json:"profile_path"`
		Order      int    `json:"order"`
	} `json:"cast"`
	Crew []struct {
		ID         int    `json:"id"`
		Name       string `json:"name"`
		Job        string `json:"job"`
		ProfileURL string `json:"profile_path"`
	} `json:"crew"`
}

type genre struct {
	Name string `json:"name"`
}

type movieDetail struct {
	ID            int     `json:"id"`
	Title         string  `json:"title"`
	OriginalTitle string  `json:"original_title"`
	Overview      string  `json:"overview"`
	ReleaseDate   string  `json:"release_date"`
	Runtime       int     `json:"runtime"`
	VoteAverage   float32 `json:"vote_average"`
	VoteCount     int     `json:"vote_count"`
	Status        string  `json:"status"`
	Popularity    float32 `json:"popularity"`
	PosterPath    string  `json:"poster_path"`
	Genres        []genre `json:"genres"`
	Credits       credits `json:"credits"`
}

type tvDetail struct {
	ID             int     `json:"id"`
	Name           string  `json:"name"`
	OriginalName   string  `json:"original_name"`
	Overview       string  `json:"overview"`
	FirstAirDate   string  `json:"first_air_date"`
	EpisodeRunTime []int   `json:"episode_run_time"`
	VoteAverage    float32 `json:"vote_average"`
	VoteCount      int     `json:"vote_count"`
	Status         string  `json:"status"`
	Popularity     float32 `json:"popularity"`
	PosterPath     string  `json:"poster_path"`
	Genres         []genre `json:"genres"`
	Credits        credits `json:"credits"`
	CreatedBy      []struct {
		ID         int    `json:"id"`
		Name       string `json:"name"`
		ProfileURL string `json:"profile_path"`
	} `json:"created_by"`
	Seasons []struct {
		SeasonNumber int    `json:"season_number"`
		Name         string `json:"name"`
		AirDate      string `json:"air_date"`
		EpisodeCount int    `json:"episode_count"`
	} `json:"seasons"`
}

// FetchPopular busca até `count` títulos populares do `kind` (filme ou série) com
// detalhes+créditos, em ordem de popularidade. Respeita rate limiting (429).
func (c *Client) FetchPopular(ctx context.Context, kind Kind, count int) ([]Title, error) {
	if c.Token == "" {
		return nil, fmt.Errorf("tmdb: token de leitura ausente (defina TMDB_READ_TOKEN)")
	}
	if count <= 0 {
		return []Title{}, nil
	}
	discoverPath := "/discover/movie"
	if kind == KindSeries {
		discoverPath = "/discover/tv"
	}
	ids, err := c.discoverIDs(ctx, discoverPath, count)
	if err != nil {
		return nil, err
	}
	return c.fetchDetails(ctx, kind, ids)
}

// discoverIDs pagina por /discover (popularidade desc) coletando ids em ordem de
// rank até ter `count`. Páginas são buscadas concorrentemente.
func (c *Client) discoverIDs(ctx context.Context, path string, count int) ([]int, error) {
	pages := (count + resultsPerPage - 1) / resultsPerPage
	if pages > maxDiscoverPage {
		pages = maxDiscoverPage
	}

	pageIDs := make([][]int, pages)
	jobs := make(chan int, pages)
	for p := 1; p <= pages; p++ {
		jobs <- p
	}
	close(jobs)

	var wg sync.WaitGroup
	var mu sync.Mutex
	var firstErr error
	workers := discoverWorkers
	if workers > pages {
		workers = pages
	}
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for page := range jobs {
				if ctx.Err() != nil {
					return
				}
				q := url.Values{}
				q.Set("sort_by", "popularity.desc")
				q.Set("vote_count.gte", "100")
				q.Set("include_adult", "false")
				q.Set("language", "pt-BR")
				q.Set("page", strconv.Itoa(page))

				var dr discoverResponse
				if err := c.getJSON(ctx, path, q, &dr); err != nil {
					mu.Lock()
					if firstErr == nil {
						firstErr = fmt.Errorf("discover page %d: %w", page, err)
					}
					mu.Unlock()
					continue
				}
				ids := make([]int, 0, len(dr.Results))
				for _, r := range dr.Results {
					ids = append(ids, r.ID)
				}
				mu.Lock()
				pageIDs[page-1] = ids
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	ids := make([]int, 0, count)
	seen := make(map[int]struct{}, count)
	for _, pageList := range pageIDs {
		for _, id := range pageList {
			if _, dup := seen[id]; dup {
				continue
			}
			seen[id] = struct{}{}
			ids = append(ids, id)
			if len(ids) >= count {
				return ids, nil
			}
		}
	}
	if len(ids) == 0 && firstErr != nil {
		return nil, firstErr
	}
	return ids, nil
}

// fetchDetails distribui as chamadas de detalhe num pool de workers e retorna os
// títulos na mesma ordem de `ids`. Falhas individuais são ignoradas.
func (c *Client) fetchDetails(ctx context.Context, kind Kind, ids []int) ([]Title, error) {
	results := make([]Title, len(ids))
	ok := make([]bool, len(ids))

	var wg sync.WaitGroup
	jobs := make(chan int, len(ids))
	for i := range ids {
		jobs <- i
	}
	close(jobs)

	var mu sync.Mutex
	var firstErr error

	for w := 0; w < detailWorkers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				if ctx.Err() != nil {
					return
				}
				t, err := c.fetchOne(ctx, kind, ids[i])
				if err != nil {
					mu.Lock()
					if firstErr == nil {
						firstErr = err
					}
					mu.Unlock()
					continue
				}
				results[i] = t
				ok[i] = true
			}
		}()
	}
	wg.Wait()

	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	titles := make([]Title, 0, len(ids))
	for i := range ids {
		if ok[i] {
			titles = append(titles, results[i])
		}
	}
	if len(titles) == 0 && firstErr != nil {
		return nil, firstErr
	}
	return titles, nil
}

func (c *Client) fetchOne(ctx context.Context, kind Kind, id int) (Title, error) {
	q := url.Values{}
	q.Set("append_to_response", "credits")
	q.Set("language", "pt-BR")

	if kind == KindSeries {
		body, err := c.get(ctx, "/tv/"+strconv.Itoa(id), q)
		if err != nil {
			return Title{}, fmt.Errorf("tv %d: %w", id, err)
		}
		var d tvDetail
		if err := json.Unmarshal(body, &d); err != nil {
			return Title{}, fmt.Errorf("parse tv %d: %w", id, err)
		}
		return d.toTitle(), nil
	}

	body, err := c.get(ctx, "/movie/"+strconv.Itoa(id), q)
	if err != nil {
		return Title{}, fmt.Errorf("movie %d: %w", id, err)
	}
	var d movieDetail
	if err := json.Unmarshal(body, &d); err != nil {
		return Title{}, fmt.Errorf("parse movie %d: %w", id, err)
	}
	return d.toTitle(), nil
}

func genreNames(gs []genre) []string {
	out := make([]string, 0, len(gs))
	for _, g := range gs {
		if g.Name != "" {
			out = append(out, g.Name)
		}
	}
	return out
}

// castPeople converte o elenco (ordenado por "order", limitado a topCast).
func castPeople(cr credits) []Person {
	cast := append([]struct {
		ID         int    `json:"id"`
		Name       string `json:"name"`
		Character  string `json:"character"`
		ProfileURL string `json:"profile_path"`
		Order      int    `json:"order"`
	}(nil), cr.Cast...)
	sort.SliceStable(cast, func(i, j int) bool { return cast[i].Order < cast[j].Order })

	out := make([]Person, 0, topCast)
	for _, m := range cast {
		if m.Name == "" {
			continue
		}
		out = append(out, Person{
			TMDBID:     m.ID,
			Nome:       m.Name,
			FotoPath:   m.ProfileURL,
			Papel:      "elenco",
			Personagem: m.Character,
			Ordem:      m.Order,
		})
		if len(out) >= topCast {
			break
		}
	}
	return out
}

func yearOf(date string) int {
	if len(date) >= 4 {
		if y, err := strconv.Atoi(date[:4]); err == nil {
			return y
		}
	}
	return 0
}

func (d movieDetail) toTitle() Title {
	people := castPeople(d.Credits)
	for _, cw := range d.Credits.Crew {
		if cw.Job == "Director" && cw.Name != "" {
			people = append(people, Person{TMDBID: cw.ID, Nome: cw.Name, FotoPath: cw.ProfileURL, Papel: "direcao"})
		}
	}
	return Title{
		Tipo:            KindMovie,
		TMDBID:          d.ID,
		Titulo:          d.Title,
		TituloOrig:      d.OriginalTitle,
		Sinopse:         d.Overview,
		Ano:             yearOf(d.ReleaseDate),
		Generos:         genreNames(d.Genres),
		PosterPath:      d.PosterPath,
		DuracaoMin:      d.Runtime,
		Popularidade:    d.Popularity,
		NotaMedia:       d.VoteAverage,
		TotalAvaliacoes: d.VoteCount,
		Status:          d.Status,
		Creditos:        people,
	}
}

func (d tvDetail) toTitle() Title {
	people := castPeople(d.Credits)
	for _, cb := range d.CreatedBy {
		if cb.Name != "" {
			people = append(people, Person{TMDBID: cb.ID, Nome: cb.Name, FotoPath: cb.ProfileURL, Papel: "direcao"})
		}
	}

	runtime := 0
	if len(d.EpisodeRunTime) > 0 {
		runtime = d.EpisodeRunTime[0]
	}

	seasons := make([]Season, 0, len(d.Seasons))
	for _, s := range d.Seasons {
		if s.SeasonNumber <= 0 { // ignora "Especiais" (temporada 0)
			continue
		}
		name := s.Name
		if name == "" {
			name = fmt.Sprintf("Temporada %d", s.SeasonNumber)
		}
		seasons = append(seasons, Season{
			Numero:         s.SeasonNumber,
			Nome:           name,
			Ano:            yearOf(s.AirDate),
			TotalEpisodios: s.EpisodeCount,
		})
	}

	return Title{
		Tipo:            KindSeries,
		TMDBID:          d.ID,
		Titulo:          d.Name,
		TituloOrig:      d.OriginalName,
		Sinopse:         d.Overview,
		Ano:             yearOf(d.FirstAirDate),
		Generos:         genreNames(d.Genres),
		PosterPath:      d.PosterPath,
		DuracaoMin:      runtime,
		Popularidade:    d.Popularity,
		NotaMedia:       d.VoteAverage,
		TotalAvaliacoes: d.VoteCount,
		Status:          d.Status,
		Creditos:        people,
		Temporadas:      seasons,
	}
}

// getJSON GETs path com query e decodifica o corpo JSON em out.
func (c *Client) getJSON(ctx context.Context, path string, q url.Values, out any) error {
	body, err := c.get(ctx, path, q)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("decode %s: %w", path, err)
	}
	return nil
}

// get faz um GET autenticado com retry em 429 (honrando Retry-After) e 5xx
// transitório, retornando o corpo da resposta.
func (c *Client) get(ctx context.Context, path string, q url.Values) ([]byte, error) {
	u := baseURL + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}

	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+c.Token)
		req.Header.Set("Accept", "application/json")

		resp, err := c.HTTP.Do(req)
		if err != nil {
			lastErr = err
			if !sleepBackoff(ctx, attempt) {
				return nil, err
			}
			continue
		}

		switch {
		case resp.StatusCode == http.StatusOK:
			body, rerr := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			if rerr != nil {
				return nil, rerr
			}
			return body, nil
		case resp.StatusCode == http.StatusTooManyRequests:
			wait := retryAfter(resp.Header.Get("Retry-After"))
			_ = resp.Body.Close()
			lastErr = fmt.Errorf("429 rate limited")
			if !sleepDur(ctx, wait) {
				return nil, ctx.Err()
			}
		case resp.StatusCode >= 500:
			_ = resp.Body.Close()
			lastErr = fmt.Errorf("status %d", resp.StatusCode)
			if !sleepBackoff(ctx, attempt) {
				return nil, lastErr
			}
		default:
			snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
			_ = resp.Body.Close()
			return nil, fmt.Errorf("status %d: %s", resp.StatusCode, strings.TrimSpace(string(snippet)))
		}
	}
	return nil, fmt.Errorf("desistindo após %d retries: %w", maxRetries, lastErr)
}

// retryAfter interpreta o header Retry-After (segundos), com default de 2s.
func retryAfter(h string) time.Duration {
	if s, err := strconv.Atoi(strings.TrimSpace(h)); err == nil && s > 0 {
		return time.Duration(s) * time.Second
	}
	return 2 * time.Second
}

func sleepBackoff(ctx context.Context, attempt int) bool {
	d := time.Duration(1<<attempt) * 500 * time.Millisecond
	return sleepDur(ctx, d)
}

func sleepDur(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}
