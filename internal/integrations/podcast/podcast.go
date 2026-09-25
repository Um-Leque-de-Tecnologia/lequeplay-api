// Package podcast é o cliente de ingestão de podcasts, usado pelo pipeline de
// seed. Como a TMDB não tem podcasts, a descoberta usa o feed público de "top
// podcasts" da Apple (Brasil), resolve a URL do feed RSS via iTunes Lookup e
// parseia o RSS (título, sinopse, capa, apresentador, frequência e episódios).
//
// Os títulos são devolvidos como tmdb.Title (Tipo=KindPodcast) para atravessar o
// mesmo pipeline de embeddings/upsert do seed.
package podcast

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Um-Leque-de-Tecnologia/lequeplay-api/internal/integrations/tmdb"
)

const (
	// marketingTopURL é o feed público de top podcasts por país (sem auth).
	marketingTopURL = "https://rss.marketingtools.apple.com/api/v2/br/podcasts/top/%d/podcasts.json"
	// lookupURL resolve metadados (inclusive feedUrl) por id(s) de coleção.
	lookupURL = "https://itunes.apple.com/lookup"

	feedWorkers = 8
	maxEpisodes = 50
	httpTimeout = 20 * time.Second
)

// Client busca e parseia podcasts a partir da Apple/iTunes.
type Client struct {
	HTTP *http.Client
}

// New constrói um cliente de podcast.
func New() *Client {
	return &Client{HTTP: &http.Client{Timeout: httpTimeout}}
}

// FetchPopular descobre os `count` podcasts mais populares no Brasil, resolve seus
// feeds RSS e retorna os títulos já mapeados para o domínio. Feeds que falham são
// ignorados (best-effort), preservando a ordem de popularidade.
func (c *Client) FetchPopular(ctx context.Context, count int) ([]tmdb.Title, error) {
	if count <= 0 {
		return []tmdb.Title{}, nil
	}
	ids, err := c.topIDs(ctx, count)
	if err != nil {
		return nil, fmt.Errorf("descobrir top podcasts: %w", err)
	}
	metas, err := c.lookup(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("lookup de podcasts: %w", err)
	}
	// Preserva a ordem de popularidade dos ids ao montar a lista de feeds.
	ordered := make([]podcastMeta, 0, len(ids))
	for _, id := range ids {
		if m, ok := metas[id]; ok && m.FeedURL != "" {
			ordered = append(ordered, m)
		}
	}
	return c.fetchFeeds(ctx, ordered), nil
}

// FetchByFeeds parseia uma lista explícita de URLs de feed RSS (override curado,
// via PODCAST_FEEDS). Não depende da API da Apple.
func (c *Client) FetchByFeeds(ctx context.Context, feedURLs []string) ([]tmdb.Title, error) {
	metas := make([]podcastMeta, 0, len(feedURLs))
	for _, u := range feedURLs {
		u = strings.TrimSpace(u)
		if u != "" {
			metas = append(metas, podcastMeta{FeedURL: u})
		}
	}
	return c.fetchFeeds(ctx, metas), nil
}

// podcastMeta são os metadados de um podcast obtidos na descoberta/lookup.
type podcastMeta struct {
	ID         int
	Nome       string
	Artista    string
	ArtworkURL string
	Genero     string
	FeedURL    string
}

// --- descoberta (Apple marketing) ---

type marketingResponse struct {
	Feed struct {
		Results []struct {
			ID string `json:"id"`
		} `json:"results"`
	} `json:"feed"`
}

func (c *Client) topIDs(ctx context.Context, count int) ([]int, error) {
	var mr marketingResponse
	if err := c.getJSON(ctx, fmt.Sprintf(marketingTopURL, count), &mr); err != nil {
		return nil, err
	}
	ids := make([]int, 0, len(mr.Feed.Results))
	for _, r := range mr.Feed.Results {
		if id, err := strconv.Atoi(r.ID); err == nil {
			ids = append(ids, id)
		}
	}
	return ids, nil
}

// --- lookup (feedUrl + metadados) ---

type lookupResponse struct {
	Results []struct {
		CollectionID     int    `json:"collectionId"`
		CollectionName   string `json:"collectionName"`
		ArtistName       string `json:"artistName"`
		FeedURL          string `json:"feedUrl"`
		ArtworkURL600    string `json:"artworkUrl600"`
		PrimaryGenreName string `json:"primaryGenreName"`
	} `json:"results"`
}

func (c *Client) lookup(ctx context.Context, ids []int) (map[int]podcastMeta, error) {
	out := make(map[int]podcastMeta, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	strIDs := make([]string, len(ids))
	for i, id := range ids {
		strIDs[i] = strconv.Itoa(id)
	}
	q := url.Values{}
	q.Set("id", strings.Join(strIDs, ","))
	q.Set("entity", "podcast")
	q.Set("country", "BR")

	var lr lookupResponse
	if err := c.getJSON(ctx, lookupURL+"?"+q.Encode(), &lr); err != nil {
		return nil, err
	}
	for _, r := range lr.Results {
		out[r.CollectionID] = podcastMeta{
			ID:         r.CollectionID,
			Nome:       r.CollectionName,
			Artista:    r.ArtistName,
			ArtworkURL: r.ArtworkURL600,
			Genero:     r.PrimaryGenreName,
			FeedURL:    r.FeedURL,
		}
	}
	return out, nil
}

// --- feeds RSS ---

func (c *Client) fetchFeeds(ctx context.Context, metas []podcastMeta) []tmdb.Title {
	results := make([]tmdb.Title, len(metas))
	ok := make([]bool, len(metas))

	jobs := make(chan int, len(metas))
	for i := range metas {
		jobs <- i
	}
	close(jobs)

	workers := feedWorkers
	if workers > len(metas) {
		workers = len(metas)
	}
	done := make(chan struct{}, workers)
	for w := 0; w < workers; w++ {
		go func() {
			defer func() { done <- struct{}{} }()
			for i := range jobs {
				if ctx.Err() != nil {
					return
				}
				t, err := c.fetchFeed(ctx, metas[i])
				if err != nil {
					continue // best-effort: ignora feed com erro
				}
				results[i] = t
				ok[i] = true
			}
		}()
	}
	for w := 0; w < workers; w++ {
		<-done
	}

	titles := make([]tmdb.Title, 0, len(metas))
	for i := range metas {
		if ok[i] {
			titles = append(titles, results[i])
		}
	}
	return titles
}

func (c *Client) fetchFeed(ctx context.Context, meta podcastMeta) (tmdb.Title, error) {
	body, err := c.get(ctx, meta.FeedURL)
	if err != nil {
		return tmdb.Title{}, err
	}
	return parseFeed(body, meta)
}

// --- parsing (puro, testável) ---

type rssFeed struct {
	Channel rssChannel `xml:"channel"`
}

type rssChannel struct {
	Title        string           `xml:"title"`
	Description  string           `xml:"description"`
	Author       string           `xml:"http://www.itunes.com/dtds/podcast-1.0.dtd author"`
	Summary      string           `xml:"http://www.itunes.com/dtds/podcast-1.0.dtd summary"`
	ItunesImage  itunesImage      `xml:"http://www.itunes.com/dtds/podcast-1.0.dtd image"`
	Image        rssImage         `xml:"image"`
	UpdatePeriod string           `xml:"http://purl.org/rss/1.0/modules/syndication/ updatePeriod"`
	Categories   []itunesCategory `xml:"http://www.itunes.com/dtds/podcast-1.0.dtd category"`
	Items        []rssItem        `xml:"item"`
}

type itunesImage struct {
	Href string `xml:"href,attr"`
}

type rssImage struct {
	URL string `xml:"url"`
}

type itunesCategory struct {
	Text string `xml:"text,attr"`
}

type rssItem struct {
	Title    string `xml:"title"`
	PubDate  string `xml:"pubDate"`
	Duration string `xml:"http://www.itunes.com/dtds/podcast-1.0.dtd duration"`
	Episode  int    `xml:"http://www.itunes.com/dtds/podcast-1.0.dtd episode"`
}

// parseFeed transforma o XML de um feed RSS num tmdb.Title de podcast, usando os
// metadados da Apple como fallback para nome/capa/gênero.
func parseFeed(body []byte, meta podcastMeta) (tmdb.Title, error) {
	var feed rssFeed
	if err := xml.Unmarshal(body, &feed); err != nil {
		return tmdb.Title{}, fmt.Errorf("parse rss: %w", err)
	}
	ch := feed.Channel

	titulo := firstNonEmpty(ch.Title, meta.Nome)
	if titulo == "" {
		return tmdb.Title{}, fmt.Errorf("feed sem título")
	}

	poster := firstNonEmpty(ch.ItunesImage.Href, ch.Image.URL, meta.ArtworkURL)
	generos := categoryNames(ch.Categories)
	if len(generos) == 0 && meta.Genero != "" {
		generos = []string{meta.Genero}
	}

	episodios := parseEpisodes(ch.Items)
	frequencia := frequencyFrom(ch.UpdatePeriod, episodios)

	var creditos []tmdb.Person
	if host := firstNonEmpty(ch.Author, meta.Artista); host != "" {
		creditos = append(creditos, tmdb.Person{Nome: host, Papel: "apresentacao"})
	}

	dur := 0
	if len(episodios) > 0 {
		dur = medianDuration(episodios)
	}

	return tmdb.Title{
		Tipo:       tmdb.KindPodcast,
		TMDBID:     meta.ID,
		Titulo:     titulo,
		Sinopse:    cleanText(firstNonEmpty(ch.Summary, ch.Description)),
		Generos:    generos,
		PosterPath: poster,
		DuracaoMin: dur,
		Creditos:   creditos,
		Frequencia: frequencia,
		Episodios:  episodios,
	}, nil
}

// parseEpisodes converte os itens do feed em episódios, limitados aos maxEpisodes
// mais recentes e ordenados por data de publicação (mais novo primeiro).
func parseEpisodes(items []rssItem) []tmdb.Episode {
	eps := make([]tmdb.Episode, 0, len(items))
	for _, it := range items {
		title := cleanText(it.Title)
		if title == "" {
			continue
		}
		eps = append(eps, tmdb.Episode{
			Numero:      it.Episode,
			Titulo:      title,
			DuracaoMin:  parseDuration(it.Duration),
			PublicadoEm: parsePubDate(it.PubDate),
		})
	}
	sort.SliceStable(eps, func(i, j int) bool {
		return eps[i].PublicadoEm.After(eps[j].PublicadoEm)
	})
	if len(eps) > maxEpisodes {
		eps = eps[:maxEpisodes]
	}
	return eps
}

// parseDuration interpreta a duração do episódio ("HH:MM:SS", "MM:SS" ou segundos)
// e devolve minutos arredondados. 0 quando ausente/ilegível.
func parseDuration(s string) int {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	var secs int
	if strings.Contains(s, ":") {
		parts := strings.Split(s, ":")
		for _, p := range parts {
			n, err := strconv.Atoi(strings.TrimSpace(p))
			if err != nil {
				return 0
			}
			secs = secs*60 + n
		}
	} else {
		n, err := strconv.Atoi(s)
		if err != nil {
			return 0
		}
		secs = n
	}
	if secs <= 0 {
		return 0
	}
	return (secs + 30) / 60 // arredonda para o minuto mais próximo
}

// pubDateLayouts são os formatos de data aceitos em <pubDate> (RFC 822/1123 e variações).
var pubDateLayouts = []string{
	time.RFC1123Z,
	time.RFC1123,
	"Mon, 2 Jan 2006 15:04:05 -0700",
	"Mon, 2 Jan 2006 15:04:05 MST",
	"2 Jan 2006 15:04:05 -0700",
	time.RFC822Z,
	time.RFC822,
}

// parsePubDate interpreta a data de publicação; devolve o zero time.Time se falhar.
func parsePubDate(s string) time.Time {
	s = strings.TrimSpace(s)
	for _, layout := range pubDateLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

// frequencyFrom deriva a periodicidade: prioriza <sy:updatePeriod>; senão infere
// pela mediana do intervalo entre publicações.
func frequencyFrom(updatePeriod string, eps []tmdb.Episode) string {
	if f := updatePeriodToFrequencia(updatePeriod); f != "" {
		return f
	}
	return inferFrequency(eps)
}

func updatePeriodToFrequencia(p string) string {
	switch strings.ToLower(strings.TrimSpace(p)) {
	case "hourly", "daily":
		return "Diário"
	case "weekly":
		return "Semanal"
	case "monthly":
		return "Mensal"
	case "yearly":
		return "Anual"
	default:
		return ""
	}
}

// inferFrequency estima a periodicidade pela mediana dos intervalos (em dias) entre
// episódios com data conhecida. Devolve "" quando não há dados suficientes.
func inferFrequency(eps []tmdb.Episode) string {
	dates := make([]time.Time, 0, len(eps))
	for _, e := range eps {
		if !e.PublicadoEm.IsZero() {
			dates = append(dates, e.PublicadoEm)
		}
	}
	if len(dates) < 2 {
		return ""
	}
	sort.Slice(dates, func(i, j int) bool { return dates[i].After(dates[j]) })
	gaps := make([]float64, 0, len(dates)-1)
	for i := 0; i+1 < len(dates); i++ {
		gaps = append(gaps, dates[i].Sub(dates[i+1]).Hours()/24)
	}
	sort.Float64s(gaps)
	median := gaps[len(gaps)/2]
	switch {
	case median <= 2:
		return "Diário"
	case median <= 10:
		return "Semanal"
	case median <= 20:
		return "Quinzenal"
	case median <= 45:
		return "Mensal"
	default:
		return ""
	}
}

func medianDuration(eps []tmdb.Episode) int {
	durs := make([]int, 0, len(eps))
	for _, e := range eps {
		if e.DuracaoMin > 0 {
			durs = append(durs, e.DuracaoMin)
		}
	}
	if len(durs) == 0 {
		return 0
	}
	sort.Ints(durs)
	return durs[len(durs)/2]
}

func categoryNames(cats []itunesCategory) []string {
	out := make([]string, 0, len(cats))
	seen := make(map[string]struct{})
	for _, c := range cats {
		name := strings.TrimSpace(c.Text)
		if name == "" {
			continue
		}
		if _, dup := seen[name]; dup {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	return out
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// cleanText remove tags HTML simples e normaliza espaços de descrições de feed.
func cleanText(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	// Remove tags básicas (<p>, <br>, <a …>) sem trazer um parser de HTML.
	var b strings.Builder
	inTag := false
	for _, r := range s {
		switch {
		case r == '<':
			inTag = true
		case r == '>':
			inTag = false
		case !inTag:
			b.WriteRune(r)
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

// --- HTTP ---

func (c *Client) getJSON(ctx context.Context, u string, out any) error {
	body, err := c.get(ctx, u)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("decode %s: %w", u, err)
	}
	return nil
}

func (c *Client) get(ctx context.Context, u string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "lequeplay-seed/1.0")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		return nil, fmt.Errorf("status %d em %s: %s", resp.StatusCode, u, strings.TrimSpace(string(snippet)))
	}
	return io.ReadAll(resp.Body)
}
