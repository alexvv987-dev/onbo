package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"onbo/internal/agent"
	"onbo/internal/backend"
	"onbo/internal/config"
)

const (
	postgresStartupMessage = "PostgreSQL startup failed"
	redisStartupMessage    = "Redis startup failed"
	serverListeningFormat  = "Onbo listening on %s, model mode: %s"
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
	settings, err := config.LoadApp()
	if err != nil {
		return err
	}
	initCtx, cancel := context.WithTimeout(ctx, settings.Lifecycle.InitTimeout)
	defer cancel()
	store, err := backend.NewStore(initCtx, settings.DatabaseURL, settings.Mock.URL, settings.Mock.Token, settings.Mock)
	if err != nil {
		return errors.New(postgresStartupMessage)
	}
	defer store.Close()
	sessions := backend.NewRedisSessions(settings.Redis.Address, settings.Redis.Password, settings.Redis)
	defer sessions.Close()
	if err = sessions.Ping(initCtx); err != nil {
		return errors.New(redisStartupMessage)
	}
	llm, err := agent.NewFromConfig(settings.Agent, store)
	if err != nil {
		return err
	}
	server := newHTTPServer(settings.HTTP, backend.NewServer(store, sessions, llm, backend.Config{DemoMode: settings.DemoMode, LLMMode: settings.Agent.Mode, StaticDir: settings.StaticDir, API: settings.API, SessionTTL: settings.Redis.SessionTTL}))
	failure := make(chan error, 1)
	go func() { failure <- server.ListenAndServe() }()
	log.Printf(serverListeningFormat, server.Addr, settings.Agent.Mode)
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
