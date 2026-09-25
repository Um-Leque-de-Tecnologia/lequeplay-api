package docs

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/Um-Leque-de-Tecnologia/lequeplay-api/internal/platform/config"
)

func TestUIRespeitaForwardedPrefix(t *testing.T) {
	h := mount(config.Docs{Enabled: true})

	// Sem prefixo: aponta para /openapi.yaml. (html/template escapa "/" como "\/"
	// no contexto JS — o browser interpreta igual.)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/docs", nil))
	if !strings.Contains(rec.Body.String(), `url: '\/openapi.yaml'`) {
		t.Errorf("sem prefixo, esperava url: '\\/openapi.yaml'; corpo: %s", rec.Body.String())
	}

	// Com X-Forwarded-Prefix do gateway: aponta para /play/openapi.yaml.
	rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/docs", nil)
	req.Header.Set("X-Forwarded-Prefix", "/play")
	h.ServeHTTP(rec, req)
	if !strings.Contains(rec.Body.String(), `url: '\/play\/openapi.yaml'`) {
		t.Errorf("com prefixo /play, esperava url: '\\/play\\/openapi.yaml'; corpo: %s", rec.Body.String())
	}
}

func TestSanitizePrefix(t *testing.T) {
	cases := map[string]string{
		"/play":        "/play",
		"/play/":       "/play",
		"play":         "/play",
		"":             "",
		"  ":           "",
		"/a/b":         "/a/b",
		"/x'><script>": "", // injeção → descartada
		"/com espaço":  "",
	}
	for in, want := range cases {
		if got := sanitizePrefix(in); got != want {
			t.Errorf("sanitizePrefix(%q) = %q, quer %q", in, got, want)
		}
	}
}

func mount(cfg config.Docs) http.Handler {
	r := chi.NewRouter()
	NewHandler(cfg).Mount(r)
	return r
}

func TestServesSpecAndUI(t *testing.T) {
	h := mount(config.Docs{Enabled: true})

	for _, path := range []string{"/openapi.yaml", "/docs"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK {
			t.Errorf("GET %s = %d, quer 200", path, rec.Code)
		}
		if rec.Body.Len() == 0 {
			t.Errorf("GET %s corpo vazio", path)
		}
	}
}

func TestPublicHostGate(t *testing.T) {
	h := mount(config.Docs{Enabled: true, PublicHost: "docs.exemplo.com"})

	// host errado → 404
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/openapi.yaml", nil)
	req.Host = "outro.com"
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("host não permitido = %d, quer 404", rec.Code)
	}

	// host certo → 200
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/openapi.yaml", nil)
	req.Host = "docs.exemplo.com"
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("host permitido = %d, quer 200", rec.Code)
	}
}
