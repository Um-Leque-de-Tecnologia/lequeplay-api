// Package auth valida os tokens de acesso e injeta as claims no contexto, além
// de fazer o proxy do login para o Keycloak. Dois modos:
//   - dev:  confia nos headers X-Debug-* (somente desenvolvimento local).
//   - oidc: valida o JWT Bearer contra o JWKS do issuer (Keycloak).
package auth

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/coreos/go-oidc/v3/oidc"

	"github.com/Um-Leque-de-Tecnologia/lequeplay-api/internal/platform/apperr"
	"github.com/Um-Leque-de-Tecnologia/lequeplay-api/internal/platform/config"
)

// Claims são os dados de identidade extraídos do token.
type Claims struct {
	Subject  string   `json:"sub"`
	Email    string   `json:"email,omitempty"`
	Username string   `json:"username,omitempty"`
	Roles    []string `json:"roles,omitempty"`
}

// HasRole indica se as claims contêm o role informado.
func (c Claims) HasRole(role string) bool {
	for _, r := range c.Roles {
		if r == role {
			return true
		}
	}
	return false
}

type realmAccessClaims struct {
	Subject     string `json:"sub"`
	Email       string `json:"email"`
	Username    string `json:"preferred_username"`
	RealmAccess struct {
		Roles []string `json:"roles"`
	} `json:"realm_access"`
}

type ctxKey struct{}

// Authenticator valida tokens conforme o modo configurado.
type Authenticator struct {
	mode     config.AuthMode
	verifier *oidc.IDTokenVerifier // nil no modo dev
}

// New constrói o Authenticator. No modo oidc, descobre o provider e monta o verifier.
func New(ctx context.Context, cfg config.Keycloak) (*Authenticator, error) {
	if cfg.Mode == config.AuthModeDev {
		return &Authenticator{mode: cfg.Mode}, nil
	}
	provider, err := oidc.NewProvider(ctx, cfg.IssuerURL())
	if err != nil {
		return nil, fmt.Errorf("descobrir provider oidc: %w", err)
	}
	verifier := provider.Verifier(&oidc.Config{
		ClientID:          cfg.AudienceValue(),
		SkipClientIDCheck: cfg.AudienceValue() == "",
	})
	return &Authenticator{mode: cfg.Mode, verifier: verifier}, nil
}

// RequireAuth exige um usuário autenticado e injeta as claims no contexto.
func (a *Authenticator) RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims, err := a.authenticate(r)
		if err != nil {
			apperr.Write(w, r, err)
			return
		}
		ctx := context.WithValue(r.Context(), ctxKey{}, claims)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RequireRole exige, além de autenticação, um realm role específico.
func (a *Authenticator) RequireRole(role string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims, err := a.authenticate(r)
			if err != nil {
				apperr.Write(w, r, err)
				return
			}
			if !claims.HasRole(role) {
				apperr.Write(w, r, apperr.New(apperr.KindForbidden, "Acesso negado", "requer o role "+role))
				return
			}
			ctx := context.WithValue(r.Context(), ctxKey{}, claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// FromContext recupera as claims injetadas pelo middleware.
func FromContext(ctx context.Context) (Claims, bool) {
	c, ok := ctx.Value(ctxKey{}).(Claims)
	return c, ok
}

func (a *Authenticator) authenticate(r *http.Request) (Claims, error) {
	if a.mode == config.AuthModeDev {
		sub := r.Header.Get("X-Debug-Subject")
		if sub == "" {
			return Claims{}, apperr.New(apperr.KindUnauthorized, "Não autenticado",
				"informe X-Debug-Subject no modo dev")
		}
		var roles []string
		if rs := strings.TrimSpace(r.Header.Get("X-Debug-Roles")); rs != "" {
			for _, p := range strings.Split(rs, ",") {
				if p = strings.TrimSpace(p); p != "" {
					roles = append(roles, p)
				}
			}
		}
		return Claims{
			Subject:  sub,
			Email:    r.Header.Get("X-Debug-Email"),
			Username: r.Header.Get("X-Debug-Username"),
			Roles:    roles,
		}, nil
	}

	raw, ok := bearerToken(r)
	if !ok {
		return Claims{}, apperr.New(apperr.KindUnauthorized, "Não autenticado", "token Bearer ausente")
	}
	return a.ClaimsFromToken(r.Context(), raw)
}

// ClaimsFromToken valida um access token e extrai as claims. Exposto para reuso
// (ex.: endpoint /me após um login).
func (a *Authenticator) ClaimsFromToken(ctx context.Context, raw string) (Claims, error) {
	if a.mode == config.AuthModeDev {
		return Claims{}, apperr.New(apperr.KindUnauthorized, "Indisponível", "validação de token não se aplica no modo dev")
	}
	idToken, err := a.verifier.Verify(ctx, raw)
	if err != nil {
		return Claims{}, apperr.New(apperr.KindUnauthorized, "Token inválido", err.Error())
	}
	var rc realmAccessClaims
	if err := idToken.Claims(&rc); err != nil {
		return Claims{}, apperr.Wrap(apperr.KindUnauthorized, "Token sem claims", err)
	}
	return Claims{
		Subject:  rc.Subject,
		Email:    rc.Email,
		Username: rc.Username,
		Roles:    rc.RealmAccess.Roles,
	}, nil
}

func bearerToken(r *http.Request) (string, bool) {
	h := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if len(h) > len(prefix) && strings.EqualFold(h[:len(prefix)], prefix) {
		return strings.TrimSpace(h[len(prefix):]), true
	}
	return "", false
}
