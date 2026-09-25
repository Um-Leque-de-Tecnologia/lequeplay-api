// Package docs serve a especificação OpenAPI e a UI do Swagger.
package docs

import (
	"html/template"
	"net/http"
	"strings"

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
	// Atrás do gateway (Gravitee) o path público tem um prefixo (ex.: /play) que é
	// removido antes de encaminhar. O Swagger UI precisa buscar o spec no caminho
	// público, então prefixamos a URL com o X-Forwarded-Prefix injetado pelo gateway.
	specURL := prefixedPath(r.Header.Get("X-Forwarded-Prefix"), "/openapi.yaml")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// html/template escapa specURL no contexto JS (defesa extra além de sanitizePrefix).
	_ = swaggerTmpl.Execute(w, map[string]string{"SpecURL": specURL})
}

// prefixedPath monta o caminho público de um recurso a partir do prefixo externo
// (X-Forwarded-Prefix, injetado pelo gateway) e do caminho interno. Sem prefixo
// válido, devolve o caminho interno como está.
func prefixedPath(prefix, path string) string {
	prefix = sanitizePrefix(prefix)
	if prefix == "" {
		return path
	}
	return prefix + path
}

// sanitizePrefix normaliza e valida o X-Forwarded-Prefix. Como o valor é
// interpolado no HTML da UI, só aceita caracteres de path seguros; qualquer coisa
// fora disso é descartada (fallback para caminho absoluto), evitando injeção.
func sanitizePrefix(p string) string {
	p = strings.TrimRight(strings.TrimSpace(p), "/")
	if p == "" {
		return ""
	}
	for _, c := range p {
		if !isSafePrefixChar(c) {
			return ""
		}
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return p
}

// isSafePrefixChar diz se a rune é aceitável num context-path de gateway.
func isSafePrefixChar(c rune) bool {
	switch {
	case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		return true
	case c == '/', c == '-', c == '_':
		return true
	default:
		return false
	}
}

// swaggerTmpl renderiza a página do Swagger UI com a URL do spec ciente do prefixo.
var swaggerTmpl = template.Must(template.New("swagger").Parse(swaggerHTML))

// swaggerHTML carrega o Swagger UI (via CDN) apontando para o spec ({{.SpecURL}},
// ciente do prefixo do gateway).
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
        url: '{{.SpecURL}}',
        dom_id: '#swagger-ui',
        presets: [SwaggerUIBundle.presets.apis],
        layout: 'BaseLayout',
        deepLinking: true,
      });
    };
  </script>
</body>
</html>`
