// Command api é o servidor HTTP da LequePlay: aplica as migrations, expõe o
// catálogo público, a busca semântica (Gemini + pgvector) e o proxy de login do
// Keycloak, além de health, métricas e tracing.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	migrations "github.com/Um-Leque-de-Tecnologia/lequeplay-api/db/migrations"
	"github.com/Um-Leque-de-Tecnologia/lequeplay-api/internal/auth"
	"github.com/Um-Leque-de-Tecnologia/lequeplay-api/internal/catalog"
	"github.com/Um-Leque-de-Tecnologia/lequeplay-api/internal/docs"
	"github.com/Um-Leque-de-Tecnologia/lequeplay-api/internal/integrations/gemini"
	"github.com/Um-Leque-de-Tecnologia/lequeplay-api/internal/platform/config"
	"github.com/Um-Leque-de-Tecnologia/lequeplay-api/internal/platform/health"
	"github.com/Um-Leque-de-Tecnologia/lequeplay-api/internal/platform/httpserver"
	"github.com/Um-Leque-de-Tecnologia/lequeplay-api/internal/platform/metrics"
	"github.com/Um-Leque-de-Tecnologia/lequeplay-api/internal/platform/postgres"
	"github.com/Um-Leque-de-Tecnologia/lequeplay-api/internal/platform/telemetry"
)

func main() {
	if err := run(); err != nil {
		slog.Error("falha fatal", slog.String("err", err.Error()))
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	logger := telemetry.NewLogger(cfg.Telemetry)
	slog.SetDefault(logger)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	shutdownTracing, err := telemetry.SetupTracing(ctx, cfg.Telemetry)
	if err != nil {
		return err
	}
	defer func() { _ = shutdownTracing(context.Background()) }()

	// Migrations: a API é a dona do schema (seguro com réplicas via advisory lock).
	if cfg.DB.MigrateOnStart {
		if err := postgres.Migrate(ctx, cfg.DB.DSN, migrations.FS); err != nil {
			return err
		}
		logger.Info("migrations aplicadas")
	}

	pool, err := postgres.NewPool(ctx, cfg.DB)
	if err != nil {
		return err
	}
	defer pool.Close()

	// Autenticação: validação de token (dev|oidc) + proxy de login do Keycloak.
	authn, err := auth.New(ctx, cfg.Keycloak)
	if err != nil {
		return err
	}
	kc := auth.NewKeycloak(cfg.Keycloak)

	// Busca semântica: Gemini como backend de embedding (e rerank opcional).
	gem := gemini.New(cfg.Gemini.APIKey)
	if !gem.Enabled() {
		logger.Warn("GEMINI_API_KEY vazia: busca cai para o modo léxico (sem vetor)")
	}
	var reranker catalog.Reranker
	if gem.Enabled() && cfg.Search.RerankEnabled {
		reranker = gem
	}

	repo := catalog.NewRepo(pool)
	searchEngine := catalog.NewSearchEngine(repo, gem, reranker, cfg.Search.RerankEnabled)

	mtr := metrics.New()
	catalogHandler := catalog.NewHandler(repo, searchEngine, mtr)
	authHandler := auth.NewHandler(kc, authn)

	hc := health.New(
		health.Checker{Name: "postgres", Check: func(ctx context.Context) error { return pool.Ping(ctx) }},
	)

	router := httpserver.NewRouter(logger)
	router.Get("/healthz", hc.Live)
	router.Get("/readyz", hc.Ready)
	router.Handle("/metrics", mtr.Handler())
	catalogHandler.Mount(router)
	authHandler.Mount(router)
	if cfg.Docs.Enabled {
		docs.NewHandler(cfg.Docs).Mount(router)
		logger.Info("documentação habilitada", slog.String("rota", "/docs"))
	}

	srv := httpserver.New(httpserver.Options{
		Name:            "api",
		Addr:            cfg.HTTP.Addr(),
		Handler:         router,
		ReadTimeout:     cfg.HTTP.ReadTimeout,
		WriteTimeout:    cfg.HTTP.WriteTimeout,
		IdleTimeout:     cfg.HTTP.IdleTimeout,
		ShutdownTimeout: cfg.HTTP.ShutdownTimeout,
	}, logger)

	return srv.Run(ctx)
}
