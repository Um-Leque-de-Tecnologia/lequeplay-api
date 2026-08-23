package auth

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/Um-Leque-de-Tecnologia/lequeplay-api/internal/platform/apperr"
)

// Handler expõe os endpoints de autenticação (proxy do Keycloak).
type Handler struct {
	kc   *Keycloak
	auth *Authenticator
}

// NewHandler constrói o handler de autenticação.
func NewHandler(kc *Keycloak, auth *Authenticator) *Handler {
	return &Handler{kc: kc, auth: auth}
}

// Mount registra as rotas de autenticação. O /me exige token válido.
func (h *Handler) Mount(r chi.Router) {
	r.Post("/v1/auth/login", h.login)
	r.Post("/v1/auth/refresh", h.refresh)
	r.Post("/v1/auth/logout", h.logout)
	r.With(h.auth.RequireAuth).Get("/v1/auth/me", h.me)
}

type loginRequest struct {
	Usuario string `json:"usuario"`
	Senha   string `json:"senha"`
}

func (h *Handler) login(w http.ResponseWriter, r *http.Request) {
	var body loginRequest
	if err := decodeJSON(r, &body); err != nil {
		apperr.Write(w, r, err)
		return
	}
	if body.Usuario == "" || body.Senha == "" {
		apperr.Write(w, r, apperr.New(apperr.KindValidation, "Dados incompletos", "usuario e senha são obrigatórios"))
		return
	}
	tok, err := h.kc.Login(r.Context(), body.Usuario, body.Senha)
	if err != nil {
		apperr.Write(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, tok)
}

type refreshRequest struct {
	RefreshToken string `json:"refreshToken"`
}

func (h *Handler) refresh(w http.ResponseWriter, r *http.Request) {
	var body refreshRequest
	if err := decodeJSON(r, &body); err != nil {
		apperr.Write(w, r, err)
		return
	}
	if body.RefreshToken == "" {
		apperr.Write(w, r, apperr.New(apperr.KindValidation, "Dados incompletos", "refreshToken é obrigatório"))
		return
	}
	tok, err := h.kc.Refresh(r.Context(), body.RefreshToken)
	if err != nil {
		apperr.Write(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, tok)
}

func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	var body refreshRequest
	if err := decodeJSON(r, &body); err != nil {
		apperr.Write(w, r, err)
		return
	}
	if body.RefreshToken == "" {
		apperr.Write(w, r, apperr.New(apperr.KindValidation, "Dados incompletos", "refreshToken é obrigatório"))
		return
	}
	if err := h.kc.Logout(r.Context(), body.RefreshToken); err != nil {
		apperr.Write(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) me(w http.ResponseWriter, r *http.Request) {
	claims, ok := FromContext(r.Context())
	if !ok {
		apperr.Write(w, r, apperr.New(apperr.KindUnauthorized, "Não autenticado", "sem claims no contexto"))
		return
	}
	writeJSON(w, http.StatusOK, claims)
}

func decodeJSON(r *http.Request, v any) error {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		return apperr.New(apperr.KindValidation, "JSON inválido", err.Error())
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
