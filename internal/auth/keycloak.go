package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Um-Leque-de-Tecnologia/lequeplay-api/internal/platform/apperr"
	"github.com/Um-Leque-de-Tecnologia/lequeplay-api/internal/platform/config"
)

// Keycloak faz o proxy do login/refresh/logout para o servidor Keycloak usando o
// client confidencial da API (Direct Access Grants / password grant).
type Keycloak struct {
	tokenURL     string
	logoutURL    string
	clientID     string
	clientSecret string
	http         *http.Client
}

// NewKeycloak constrói o cliente de proxy a partir da configuração.
func NewKeycloak(cfg config.Keycloak) *Keycloak {
	base := strings.TrimRight(cfg.BaseURL, "/") + "/realms/" + cfg.Realm + "/protocol/openid-connect"
	return &Keycloak{
		tokenURL:     base + "/token",
		logoutURL:    base + "/logout",
		clientID:     cfg.ClientID,
		clientSecret: cfg.ClientSecret,
		http:         &http.Client{Timeout: 10 * time.Second},
	}
}

// Token é a resposta de token do Keycloak (repassada ao cliente).
type Token struct {
	AccessToken      string `json:"accessToken"`
	ExpiresIn        int    `json:"expiresIn"`
	RefreshToken     string `json:"refreshToken"`
	RefreshExpiresIn int    `json:"refreshExpiresIn"`
	TokenType        string `json:"tokenType"`
	Scope            string `json:"scope,omitempty"`
}

type keycloakToken struct {
	AccessToken      string `json:"access_token"`
	ExpiresIn        int    `json:"expires_in"`
	RefreshToken     string `json:"refresh_token"`
	RefreshExpiresIn int    `json:"refresh_expires_in"`
	TokenType        string `json:"token_type"`
	Scope            string `json:"scope"`
}

func (t keycloakToken) toToken() Token {
	return Token(t)
}

// Login troca usuário/senha por tokens (grant_type=password).
func (k *Keycloak) Login(ctx context.Context, username, password string) (Token, error) {
	form := url.Values{}
	form.Set("grant_type", "password")
	form.Set("client_id", k.clientID)
	form.Set("client_secret", k.clientSecret)
	form.Set("username", username)
	form.Set("password", password)
	form.Set("scope", "openid")
	return k.token(ctx, form)
}

// Refresh troca um refresh token por novos tokens (grant_type=refresh_token).
func (k *Keycloak) Refresh(ctx context.Context, refreshToken string) (Token, error) {
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("client_id", k.clientID)
	form.Set("client_secret", k.clientSecret)
	form.Set("refresh_token", refreshToken)
	return k.token(ctx, form)
}

// Logout revoga a sessão associada ao refresh token.
func (k *Keycloak) Logout(ctx context.Context, refreshToken string) error {
	form := url.Values{}
	form.Set("client_id", k.clientID)
	form.Set("client_secret", k.clientSecret)
	form.Set("refresh_token", refreshToken)

	resp, err := k.post(ctx, k.logoutURL, form)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		return k.mapError(resp)
	}
	return nil
}

func (k *Keycloak) token(ctx context.Context, form url.Values) (Token, error) {
	resp, err := k.post(ctx, k.tokenURL, form)
	if err != nil {
		return Token{}, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return Token{}, k.mapError(resp)
	}
	var kt keycloakToken
	if err := json.NewDecoder(resp.Body).Decode(&kt); err != nil {
		return Token{}, apperr.Wrap(apperr.KindInternal, "Resposta inválida do Keycloak", err)
	}
	return kt.toToken(), nil
}

func (k *Keycloak) post(ctx context.Context, endpoint string, form url.Values) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, apperr.Wrap(apperr.KindInternal, "Falha ao montar requisição", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := k.http.Do(req)
	if err != nil {
		return nil, apperr.Wrap(apperr.KindUnavailable, "Keycloak indisponível", err)
	}
	return resp, nil
}

// mapError converte um erro do Keycloak numa resposta de aplicação adequada.
func (k *Keycloak) mapError(resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
	var kerr struct {
		Error string `json:"error"`
		Desc  string `json:"error_description"`
	}
	_ = json.Unmarshal(body, &kerr)

	switch resp.StatusCode {
	case http.StatusUnauthorized, http.StatusBadRequest:
		detail := kerr.Desc
		if detail == "" {
			detail = "credenciais inválidas"
		}
		return apperr.New(apperr.KindUnauthorized, "Falha na autenticação", detail)
	case http.StatusTooManyRequests:
		return apperr.New(apperr.KindTooManyRequests, "Muitas tentativas", "tente novamente em instantes")
	default:
		return apperr.New(apperr.KindUnavailable, "Erro no Keycloak", fmt.Sprintf("status %d", resp.StatusCode))
	}
}
