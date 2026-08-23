// Package httpserver monta roteadores chi com middlewares base e gerencia o
// ciclo de vida de servidores HTTP (start + graceful shutdown).
package httpserver

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// NewRouter cria um roteador chi com os middlewares transversais já aplicados.
func NewRouter(logger *slog.Logger) *chi.Mux {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.Recoverer)
	r.Use(securityHeaders)
	r.Use(RequestLogger(logger))
	return r
}

// Server encapsula um *http.Server e seu ciclo de vida.
type Server struct {
	httpServer *http.Server
	logger     *slog.Logger
	shutdown   time.Duration
	name       string
}

// Options descreve os parâmetros de um servidor HTTP.
type Options struct {
	Name            string
	Addr            string
	Handler         http.Handler
	ReadTimeout     time.Duration
	WriteTimeout    time.Duration
	IdleTimeout     time.Duration
	ShutdownTimeout time.Duration
}

// New cria o servidor a partir das opções e do handler raiz.
func New(opts Options, logger *slog.Logger) *Server {
	return &Server{
		httpServer: &http.Server{
			Addr:         opts.Addr,
			Handler:      opts.Handler,
			ReadTimeout:  opts.ReadTimeout,
			WriteTimeout: opts.WriteTimeout,
			IdleTimeout:  opts.IdleTimeout,
		},
		logger:   logger,
		shutdown: opts.ShutdownTimeout,
		name:     opts.Name,
	}
}

// Run sobe o servidor e bloqueia até ctx ser cancelado, então faz graceful shutdown.
func (s *Server) Run(ctx context.Context) error {
	errCh := make(chan error, 1)
	go func() {
		s.logger.Info("http server iniciando",
			slog.String("name", s.name),
			slog.String("addr", s.httpServer.Addr),
		)
		if err := s.httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		s.logger.Info("http server desligando", slog.String("name", s.name))
		shutdownCtx, cancel := context.WithTimeout(context.Background(), s.shutdown)
		defer cancel()
		return s.httpServer.Shutdown(shutdownCtx)
	}
}
