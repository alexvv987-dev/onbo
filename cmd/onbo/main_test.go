package main

import (
	"context"
	"net"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

func TestConfigurationErrors(test *testing.T) {
	test.Setenv("LLM_MODE", "demo")
	for _, key := range []string{"DATABASE_URL", "REDIS_ADDR", "MOCK_TOKEN"} {
		test.Setenv(key, "")
	}
	if err := run(); err == nil {
		test.Fatal("missing configuration accepted")
	}
	test.Setenv("DATABASE_URL", "postgres://none:none@127.0.0.1:1/none?connect_timeout=1")
	test.Setenv("REDIS_ADDR", "127.0.0.1:1")
	test.Setenv("MOCK_TOKEN", "test-token")
	if err := run(); err == nil || !strings.Contains(err.Error(), "PostgreSQL") {
		test.Fatal(err)
	}
}
func TestStartupShutdownAndDependencyErrors(test *testing.T) {
	db := os.Getenv("ONBO_TEST_DATABASE_URL")
	redis := os.Getenv("ONBO_TEST_REDIS_ADDR")
	if db == "" || redis == "" {
		test.Skip("PostgreSQL and Redis integration configuration required")
	}
	test.Setenv("DATABASE_URL", db)
	test.Setenv("REDIS_ADDR", "127.0.0.1:1")
	test.Setenv("MOCK_TOKEN", "test-token")
	test.Setenv("REDIS_PASSWORD", "")
	test.Setenv("LLM_MODE", "demo")
	test.Setenv("DEMO_MODE", "true")
	if err := run(); err == nil || !strings.Contains(err.Error(), "Redis") {
		test.Fatal(err)
	}
	test.Setenv("REDIS_ADDR", redis)
	test.Setenv("LLM_MODE", "unknown")
	if err := run(); err == nil {
		test.Fatal("unknown model mode accepted")
	}
	test.Setenv("LLM_MODE", "demo")
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		test.Fatal(err)
	}
	address := listener.Addr().String()
	test.Setenv("HTTP_ADDR", address)
	if err := run(); err == nil {
		test.Fatal("occupied address accepted")
	}
	listener.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- serve(ctx) }()
	client := &http.Client{Timeout: time.Second}
	ready := false
	for itemIndex := 0; itemIndex < 100; itemIndex++ {
		response, err := client.Get("http://" + address + "/healthz")
		if err == nil {
			response.Body.Close()
			if response.StatusCode == 200 {
				ready = true
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !ready {
		test.Fatal("server failed readiness")
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			test.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		test.Fatal("server failed graceful shutdown")
	}
}
