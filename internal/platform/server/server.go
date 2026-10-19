package server

import (
	"context"
	"net/http"
	"time"
)

const (
	MaxHeaderBytes = 1 << 20    // 1 MB
	MaxBodyBytes   = 16 * 1024  // 16 KB per AGENTS.md Section 10
)

// Server wraps an http.Server with production timeouts and lifecycle controls.
type Server struct {
	httpServer *http.Server
}

// Config defines network timeouts and parameters for the HTTP server.
type Config struct {
	Addr         string
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
	IdleTimeout  time.Duration
}

// New creates an HTTP server configured with standard production timeouts.
func New(cfg Config, handler http.Handler) *Server {
	if cfg.ReadTimeout == 0 {
		cfg.ReadTimeout = 10 * time.Second
	}
	if cfg.WriteTimeout == 0 {
		cfg.WriteTimeout = 15 * time.Second
	}
	if cfg.IdleTimeout == 0 {
		cfg.IdleTimeout = 60 * time.Second
	}

	return &Server{
		httpServer: &http.Server{
			Addr:           cfg.Addr,
			Handler:        handler,
			ReadTimeout:    cfg.ReadTimeout,
			WriteTimeout:   cfg.WriteTimeout,
			IdleTimeout:    cfg.IdleTimeout,
			MaxHeaderBytes: MaxHeaderBytes,
		},
	}
}

// Start launches the HTTP server asynchronously and returns an error channel.
func (s *Server) Start() <-chan error {
	errChan := make(chan error, 1)
	go func() {
		if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errChan <- err
		}
		close(errChan)
	}()
	return errChan
}

// Shutdown gracefully stops the HTTP server.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.httpServer.Shutdown(ctx)
}

