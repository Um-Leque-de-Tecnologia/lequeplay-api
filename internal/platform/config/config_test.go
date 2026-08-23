package config

import "testing"

func TestKeycloakDerivations(t *testing.T) {
	k := Keycloak{BaseURL: "https://auth.example.com/", Realm: "lequeplay", ClientID: "lequeplay-api"}
	if got := k.IssuerURL(); got != "https://auth.example.com/realms/lequeplay" {
		t.Errorf("IssuerURL = %q", got)
	}
	if got := k.AudienceValue(); got != "lequeplay-api" {
		t.Errorf("AudienceValue = %q, quer lequeplay-api", got)
	}

	explicit := Keycloak{Issuer: "https://custom/issuer", Audience: "aud"}
	if explicit.IssuerURL() != "https://custom/issuer" {
		t.Errorf("issuer explícito deve prevalecer")
	}
	if explicit.AudienceValue() != "aud" {
		t.Errorf("audience explícita deve prevalecer")
	}
}

func TestLoadRequiresOIDCFields(t *testing.T) {
	t.Setenv("DB_DSN", "postgres://x")
	t.Setenv("KEYCLOAK_MODE", "oidc")
	// Sem BASE_URL/ISSUER nem CLIENT_SECRET: deve falhar.
	if _, err := Load(); err == nil {
		t.Fatal("esperava erro por faltar issuer/secret no modo oidc")
	}

	t.Setenv("KEYCLOAK_BASE_URL", "https://auth.example.com")
	t.Setenv("KEYCLOAK_CLIENT_SECRET", "s3cr3t")
	if _, err := Load(); err != nil {
		t.Fatalf("config válida deveria carregar: %v", err)
	}
}
