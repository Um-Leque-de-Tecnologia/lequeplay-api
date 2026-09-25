// Package config carrega a configuração da aplicação a partir do ambiente.
//
// Campos obrigatórios sem default fazem Load() falhar com erro claro. A fonte
// pode futuramente passar a ser o Vault sem alterar os consumidores.
package config

import (
	"fmt"
	"strings"
	"time"

	"github.com/caarlos0/env/v11"
)

// Config agrega toda a configuração da aplicação.
type Config struct {
	Env       string `env:"APP_ENV" envDefault:"dev"`
	HTTP      HTTP
	DB        DB
	Keycloak  Keycloak
	Gemini    Gemini
	TMDB      TMDB
	Podcast   Podcast
	Search    Search
	Docs      Docs
	Telemetry Telemetry
}

// Docs configura a exposição da documentação OpenAPI/Swagger.
type Docs struct {
	// Enabled liga as rotas /docs e /openapi.yaml.
	Enabled bool `env:"DOCS_ENABLED" envDefault:"true"`
	// PublicHost, quando definido, restringe a documentação a esse host (vazio =
	// qualquer host).
	PublicHost string `env:"DOCS_PUBLIC_HOST" envDefault:""`
}

// HTTP configura o servidor da API.
type HTTP struct {
	Host            string        `env:"HTTP_HOST" envDefault:"0.0.0.0"`
	Port            int           `env:"PORT" envDefault:"8080"`
	ReadTimeout     time.Duration `env:"HTTP_READ_TIMEOUT" envDefault:"15s"`
	WriteTimeout    time.Duration `env:"HTTP_WRITE_TIMEOUT" envDefault:"30s"`
	IdleTimeout     time.Duration `env:"HTTP_IDLE_TIMEOUT" envDefault:"120s"`
	ShutdownTimeout time.Duration `env:"HTTP_SHUTDOWN_TIMEOUT" envDefault:"20s"`
}

// Addr retorna o endereço de bind do servidor.
func (h HTTP) Addr() string { return fmt.Sprintf("%s:%d", h.Host, h.Port) }

// DB configura a conexão com o Postgres.
type DB struct {
	DSN            string `env:"DB_DSN,required"`
	MaxConns       int32  `env:"DB_MAX_CONNS" envDefault:"10"`
	MinConns       int32  `env:"DB_MIN_CONNS" envDefault:"2"`
	MigrateOnStart bool   `env:"DB_MIGRATE_ON_START" envDefault:"true"`
}

// AuthMode define como a API valida os tokens de acesso.
type AuthMode string

const (
	// AuthModeDev confia em headers de debug e NÃO valida assinatura. Apenas dev local.
	AuthModeDev AuthMode = "dev"
	// AuthModeOIDC valida o JWT Bearer contra o JWKS do issuer (Keycloak).
	AuthModeOIDC AuthMode = "oidc"
)

// Keycloak configura a validação OIDC e o proxy de login.
type Keycloak struct {
	Mode         AuthMode `env:"KEYCLOAK_MODE" envDefault:"dev"`
	BaseURL      string   `env:"KEYCLOAK_BASE_URL" envDefault:""`
	Realm        string   `env:"KEYCLOAK_REALM" envDefault:"lequeplay"`
	ClientID     string   `env:"KEYCLOAK_CLIENT_ID" envDefault:"lequeplay-api"`
	ClientSecret string   `env:"KEYCLOAK_CLIENT_SECRET" envDefault:""`
	// Issuer e Audience derivam de BaseURL/Realm/ClientID quando vazios; podem ser
	// sobrescritos explicitamente.
	Issuer   string `env:"KEYCLOAK_ISSUER" envDefault:""`
	Audience string `env:"KEYCLOAK_AUDIENCE" envDefault:""`
}

// IssuerURL retorna o issuer OIDC do realm, derivando de BaseURL/Realm quando não explícito.
func (k Keycloak) IssuerURL() string {
	if k.Issuer != "" {
		return k.Issuer
	}
	if k.BaseURL == "" {
		return ""
	}
	return strings.TrimRight(k.BaseURL, "/") + "/realms/" + k.Realm
}

// AudienceValue retorna a audience esperada no token (default: ClientID).
func (k Keycloak) AudienceValue() string {
	if k.Audience != "" {
		return k.Audience
	}
	return k.ClientID
}

// Gemini configura o acesso à API de embeddings do Gemini.
type Gemini struct {
	APIKey string `env:"GEMINI_API_KEY" envDefault:""`
}

// TMDB configura o acesso à API do The Movie Database (usado no seed).
type TMDB struct {
	ReadToken string `env:"TMDB_READ_TOKEN" envDefault:""`
}

// Podcast configura a ingestão de podcasts (usado no seed). Feeds, quando definido,
// sobrepõe a descoberta automática pela Apple por uma lista curada de URLs de feed RSS.
type Podcast struct {
	Feeds []string `env:"PODCAST_FEEDS" envSeparator:","`
}

// Search configura o comportamento da busca semântica.
type Search struct {
	// RerankEnabled liga o rerank via Gemini LLM sobre os candidatos fundidos.
	// Desligado por padrão para evitar latência/custo por query.
	RerankEnabled bool `env:"RERANK_ENABLED" envDefault:"false"`
}

// Telemetry configura logs e OpenTelemetry.
type Telemetry struct {
	OTelEnabled  bool   `env:"OTEL_ENABLED" envDefault:"false"`
	ServiceName  string `env:"OTEL_SERVICE_NAME" envDefault:"lequeplay-api"`
	OTLPEndpoint string `env:"OTEL_EXPORTER_OTLP_ENDPOINT" envDefault:"localhost:4317"`
	LogLevel     string `env:"LOG_LEVEL" envDefault:"info"`
}

// Load lê a configuração do ambiente e valida campos obrigatórios.
func Load() (Config, error) {
	var cfg Config
	if err := env.Parse(&cfg); err != nil {
		return Config{}, fmt.Errorf("carregar config: %w", err)
	}
	if cfg.Keycloak.Mode == AuthModeOIDC {
		if cfg.Keycloak.IssuerURL() == "" {
			return Config{}, fmt.Errorf("carregar config: KEYCLOAK_BASE_URL (ou KEYCLOAK_ISSUER) é obrigatório quando KEYCLOAK_MODE=oidc")
		}
		if cfg.Keycloak.ClientSecret == "" {
			return Config{}, fmt.Errorf("carregar config: KEYCLOAK_CLIENT_SECRET é obrigatório quando KEYCLOAK_MODE=oidc")
		}
	}
	return cfg, nil
}

// IsProd indica se a aplicação roda em produção.
func (c Config) IsProd() bool { return c.Env == "prod" }
