package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"onbo/internal/config"
	mockservice "onbo/internal/mock"
)

const (
	postgresStartupMessage = "mock PostgreSQL startup failed"
	serverListeningFormat  = "Access mock listening on %s"
)

func main() {
	if err := run(); err != nil {
		log.Print(err)
		os.Exit(1)
	}
}
func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return serve(ctx)
}

func serve(ctx context.Context) error {
	settings, err := config.LoadMock()
	if err != nil {
		return err
	}
	initCtx, cancel := context.WithTimeout(ctx, settings.Lifecycle.InitTimeout)
	defer cancel()
	mock, err := mockservice.New(initCtx, settings.DatabaseURL, settings.Token, settings)
	if err != nil {
		return errors.New(postgresStartupMessage)
	}
	defer mock.Close()
	server := newHTTPServer(settings.HTTP, mock)
	failure := make(chan error, 1)
	go func() { failure <- server.ListenAndServe() }()
	log.Printf(serverListeningFormat, settings.HTTP.Address)
	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), settings.Lifecycle.ShutdownTimeout)
		defer cancel()
		return server.Shutdown(shutdownCtx)
	case err := <-failure:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}
