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
	writeJSON(w, http.StatusOK, generos)
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

func (h *Handler) getMidia(w http.ResponseWriter, r *http.Request) {
	// O path param aceita o UUID ou o slug da mídia.
	idOrSlug := chi.URLParam(r, "id")
	midia, err := h.repo.GetMidia(r.Context(), idOrSlug)
	if err != nil {
		apperr.Write(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, midia)
}

func (h *Handler) getTemporadaEpisodios(w http.ResponseWriter, r *http.Request) {
	// O path param aceita o UUID ou o slug da mídia.
	idOrSlug := chi.URLParam(r, "id")
	numero, err := strconv.Atoi(chi.URLParam(r, "numero"))
	if err != nil || numero <= 0 {
		apperr.Write(w, r, apperr.New(apperr.KindValidation, "Temporada inválida", "número da temporada deve ser um inteiro positivo"))
		return
	}
	episodios, err := h.repo.EpisodiosDaTemporada(r.Context(), idOrSlug, numero)
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

// parseFilter extrai tipo/genero/ano/limite/offset dos query params.
func parseFilter(r *http.Request) Filter {
	q := r.URL.Query()
	f := Filter{
		Tipo:   q.Get("tipo"),
		Genero: q.Get("genero"),
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
	return f
}

// writeJSON serializa v como JSON com o status informado.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
