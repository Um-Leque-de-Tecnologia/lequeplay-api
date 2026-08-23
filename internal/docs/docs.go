// Package docs serve a especificação OpenAPI e a UI do Swagger.
package docs

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	apispec "github.com/Um-Leque-de-Tecnologia/lequeplay-api/api"
	"github.com/Um-Leque-de-Tecnologia/lequeplay-api/internal/platform/config"
)

// Handler expõe /openapi.yaml e /docs (Swagger UI).
type Handler struct {
	publicHost string
}

// NewHandler constrói o handler de docs.
func NewHandler(cfg config.Docs) *Handler {
	return &Handler{publicHost: cfg.PublicHost}
}

// Mount registra as rotas de documentação.
func (h *Handler) Mount(r chi.Router) {
	r.Get("/openapi.yaml", h.spec)
	r.Get("/docs", h.ui)
}

// allowed limita o acesso ao host público quando DOCS_PUBLIC_HOST está definido
// (vazio = qualquer host).
func (h *Handler) allowed(r *http.Request) bool {
	return h.publicHost == "" || r.Host == h.publicHost
}

func (h *Handler) spec(w http.ResponseWriter, r *http.Request) {
	if !h.allowed(r) {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
	_, _ = w.Write(apispec.OpenAPISpec)
}

func (h *Handler) ui(w http.ResponseWriter, r *http.Request) {
	if !h.allowed(r) {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(swaggerHTML))
}

// swaggerHTML carrega o Swagger UI (via CDN) apontando para /openapi.yaml.
const swaggerHTML = `<!DOCTYPE html>
<html lang="pt-BR">
<head>
  <meta charset="utf-8"/>
  <meta name="viewport" content="width=device-width, initial-scale=1"/>
  <title>LequePlay API — Docs</title>
  <link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@5/swagger-ui.css"/>
</head>
<body>
  <div id="swagger-ui"></div>
  <script src="https://unpkg.com/swagger-ui-dist@5/swagger-ui-bundle.js" crossorigin></script>
  <script>
    window.onload = function () {
      window.ui = SwaggerUIBundle({
        url: '/openapi.yaml',
        dom_id: '#swagger-ui',
        presets: [SwaggerUIBundle.presets.apis],
        layout: 'BaseLayout',
        deepLinking: true,
      });
    };
  </script>
</body>
</html>`
