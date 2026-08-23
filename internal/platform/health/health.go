// Package health expõe os endpoints de liveness e readiness.
package health

import (
	"context"
	"net/http"
	"time"
)

// Checker é uma verificação nomeada de dependência (Postgres, etc.).
type Checker struct {
	Name  string
	Check func(context.Context) error
}

// Handler agrega os checks de readiness.
type Handler struct {
	checkers []Checker
	timeout  time.Duration
}

// New cria o handler de health com os checkers informados.
func New(checkers ...Checker) *Handler {
	return &Handler{checkers: checkers, timeout: 3 * time.Second}
}

// Live responde 200 sempre que o processo está de pé.
func (h *Handler) Live(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

// Ready responde 200 só quando todas as dependências respondem.
func (h *Handler) Ready(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), h.timeout)
	defer cancel()
	for _, c := range h.checkers {
		if err := c.Check(ctx); err != nil {
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(c.Name + ": " + err.Error()))
			return
		}
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ready"))
}
