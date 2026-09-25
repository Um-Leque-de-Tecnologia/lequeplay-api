package catalog

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Um-Leque-de-Tecnologia/lequeplay-api/internal/platform/apperr"
	"github.com/Um-Leque-de-Tecnologia/lequeplay-api/internal/platform/metrics"
)

const (
	defaultLimit = 20
	maxLimit     = 100
)

// Handler expõe os endpoints públicos do catálogo e da busca.
type Handler struct {
	repo    *Repo
	search  *SearchEngine
	metrics *metrics.Metrics
}

// NewHandler constrói o handler do catálogo. `m` pode ser nil (sem instrumentação).
func NewHandler(repo *Repo, search *SearchEngine, m *metrics.Metrics) *Handler {
	return &Handler{repo: repo, search: search, metrics: m}
}

// Mount registra as rotas públicas do catálogo no roteador.
func (h *Handler) Mount(r chi.Router) {
	r.Get("/v1/generos", h.listGeneros)
	r.Get("/v1/midias", h.listMidias)
	// O caminho continua {id} para não quebrar cliente nenhum, mas o parâmetro
	// aceita o id (UUID) ou o slug da mídia.
	r.Get("/v1/midias/{id}", h.getMidia)
	r.Get("/v1/midias/{id}/temporadas/{numero}/episodios", h.getTemporadaEpisodios)
	r.Get("/v1/busca", h.busca)
	r.Get("/v1/catalogo/versao", h.catalogVersion)
}

func (h *Handler) listGeneros(w http.ResponseWriter, r *http.Request) {
	generos, err := h.repo.ListGeneros(r.Context())
	if err != nil {
		apperr.Write(w, r, err)
		return
	}
	// Envelope com `itens`, como toda listagem desta API: listagem que às vezes
	// devolve array puro e às vezes envelope é a causa nº 1 de cliente quebrado.
	//
	// E nomes, não objetos {id, nome}: o filtro do catálogo é `?genero=Drama`,
	// então o que o cliente precisa desta lista é exatamente o valor que ele vai
	// mandar de volta na query. O id do gênero não tem uso publicado.
	writeJSON(w, http.StatusOK, map[string][]string{"itens": generos})
}

func (h *Handler) listMidias(w http.ResponseWriter, r *http.Request) {
	f := parseFilter(r)
	page, err := h.repo.ListMidias(r.Context(), f)
	if err != nil {
		apperr.Write(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}

// getMidia atende GET /v1/midias/{id}, onde {id} é o id (UUID) ou o slug.
func (h *Handler) getMidia(w http.ResponseWriter, r *http.Request) {
	idOuSlug := chi.URLParam(r, "id")
	midia, err := h.repo.GetMidia(r.Context(), idOuSlug)
	if err != nil {
		apperr.Write(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, midia)
}

// getTemporadaEpisodios atende GET /v1/midias/{id}/temporadas/{numero}/episodios,
// onde {id} é o id (UUID) ou o slug da série.
func (h *Handler) getTemporadaEpisodios(w http.ResponseWriter, r *http.Request) {
	idOuSlug := chi.URLParam(r, "id")
	numero, err := strconv.Atoi(chi.URLParam(r, "numero"))
	if err != nil || numero <= 0 {
		apperr.Write(w, r, apperr.New(apperr.KindValidation, "Temporada inválida", "número da temporada deve ser um inteiro positivo"))
		return
	}
	episodios, err := h.repo.EpisodiosDaTemporada(r.Context(), idOuSlug, numero)
	if err != nil {
		apperr.Write(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, episodios)
}

func (h *Handler) busca(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	if q == "" {
		apperr.Write(w, r, apperr.New(apperr.KindValidation, "Consulta ausente", "informe o parâmetro q"))
		return
	}
	f := parseFilter(r)
	mode := SearchMode(r.URL.Query().Get("modo"))

	start := time.Now()
	res, err := h.search.Search(r.Context(), q, mode, f)
	if err != nil {
		apperr.Write(w, r, err)
		return
	}
	if h.metrics != nil {
		h.metrics.Searches.WithLabelValues(string(res.Modo)).Inc()
		h.metrics.SearchSeconds.WithLabelValues(string(res.Modo)).Observe(time.Since(start).Seconds())
		if res.UsouFallback {
			h.metrics.EmbedFallbacks.Inc()
		}
	}
	writeJSON(w, http.StatusOK, res)
}

func (h *Handler) catalogVersion(w http.ResponseWriter, r *http.Request) {
	v, err := h.repo.CatalogVersion(r.Context())
	if err != nil {
		apperr.Write(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int64{"versao": v})
}

// parseFilter extrai tipo/genero/ano/q e a paginação dos query params.
//
// A paginação publicada é `pagina`/`porPagina`: é o par que o contrato com o
// front descreve e o que a tela precisa para dizer "página 2 de 6". O par
// `limite`/`offset` continua aceito porque já estava publicado e alguém pode
// estar usando — quando os dois vêm, `pagina`/`porPagina` vence, por ser o
// documentado. A conversão para offset acontece aqui, e só aqui: o SQL lá
// dentro continua pensando em limite e offset.
func parseFilter(r *http.Request) Filter {
	q := r.URL.Query()
	f := Filter{
		Tipo:   q.Get("tipo"),
		Genero: q.Get("genero"),
		Q:      q.Get("q"),
		Limite: defaultLimit,
	}
	if a, err := strconv.Atoi(q.Get("ano")); err == nil {
		f.Ano = a
	}
	if l, err := strconv.Atoi(q.Get("limite")); err == nil && l > 0 {
		if l > maxLimit {
			l = maxLimit
		}
		f.Limite = l
	}
	if o, err := strconv.Atoi(q.Get("offset")); err == nil && o > 0 {
		f.Offset = o
	}
	if pp, err := strconv.Atoi(q.Get("porPagina")); err == nil && pp > 0 {
		if pp > maxLimit {
			pp = maxLimit
		}
		f.Limite = pp
	}
	if p, err := strconv.Atoi(q.Get("pagina")); err == nil && p > 1 {
		f.Offset = (p - 1) * f.Limite
	}
	return f
}

// writeJSON serializa v como JSON com o status informado.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
