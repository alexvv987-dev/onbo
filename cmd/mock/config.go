package main

import (
	"net/http"
	"onbo/internal/config"
)

func newHTTPServer(settings config.MockHTTP, handler http.Handler) *http.Server {
	return &http.Server{Addr: settings.Address, Handler: handler, ReadHeaderTimeout: settings.ReadHeaderTimeout, ReadTimeout: settings.ReadTimeout, WriteTimeout: settings.WriteTimeout, IdleTimeout: settings.IdleTimeout}
}
