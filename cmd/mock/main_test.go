package main

import (
	"context"
	"net"
	"net/http"
	"os"
	"testing"
	"time"
)

func TestStartupConfiguration(test *testing.T) {
	test.Setenv("DATABASE_URL", "")
	test.Setenv("MOCK_TOKEN", "")
	if err := run(); err == nil {
		test.Fatal("missing config accepted")
	}
	test.Setenv("DATABASE_URL", "postgres://none:none@127.0.0.1:1/no?connect_timeout=1")
	test.Setenv("MOCK_TOKEN", "test-token")
	if err := run(); err == nil {
		test.Fatal("missing database accepted")
	}
}
func TestServeLifecycle(test *testing.T) {
	db := os.Getenv("ONBO_TEST_DATABASE_URL")
	if db == "" {
		test.Skip("ONBO_TEST_DATABASE_URL required")
	}
	test.Setenv("DATABASE_URL", db)
	test.Setenv("MOCK_TOKEN", "test-token")
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
		test.Fatal("mock failed readiness")
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			test.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		test.Fatal("mock failed shutdown")
	}
}
