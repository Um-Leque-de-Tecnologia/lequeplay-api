// Package metrics define os coletores Prometheus da API e o handler /metrics.
package metrics

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Metrics agrega os coletores da API num registry dedicado.
type Metrics struct {
	registry *prometheus.Registry

	// Buscas realizadas, rotuladas pelo modo (vector|fts|hybrid).
	Searches *prometheus.CounterVec
	// Latência da busca fim a fim, por modo.
	SearchSeconds *prometheus.HistogramVec
	// Latência das chamadas externas (gemini|tmdb), por resultado (ok|error).
	ExternalSeconds *prometheus.HistogramVec
	// Uso do fallback de embedding (Gemini indisponível → sem vetor).
	EmbedFallbacks prometheus.Counter
}

// New cria os coletores e os registra num registry dedicado.
func New() *Metrics {
	reg := prometheus.NewRegistry()
	m := &Metrics{
		registry: reg,
		Searches: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "lequeplay_searches_total", Help: "Total de buscas, por modo.",
		}, []string{"mode"}),
		SearchSeconds: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "lequeplay_search_seconds",
			Help:    "Latência da busca fim a fim, por modo.",
			Buckets: prometheus.DefBuckets,
		}, []string{"mode"}),
		ExternalSeconds: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "lequeplay_external_call_seconds",
			Help:    "Latência de chamadas externas (gemini|tmdb), por resultado.",
			Buckets: prometheus.DefBuckets,
		}, []string{"provider", "result"}),
		EmbedFallbacks: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "lequeplay_embed_fallbacks_total", Help: "Buscas que caíram para o modo sem vetor (Gemini indisponível).",
		}),
	}
	reg.MustRegister(m.Searches, m.SearchSeconds, m.ExternalSeconds, m.EmbedFallbacks)
	// Coletores de runtime/process padrão.
	reg.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)
	return m
}

// Handler retorna o http.Handler que expõe /metrics no formato Prometheus.
func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{})
}
