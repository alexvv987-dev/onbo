package main

import (
	"net/http"
	"onbo/internal/config"
)

// newHTTPServer applies the validated HTTP settings to the application server.
func newHTTPServer(settings config.HTTP, handler http.Handler) *http.Server {
	return &http.Server{Addr: settings.Address, Handler: handler, ReadHeaderTimeout: settings.ReadHeaderTimeout, ReadTimeout: settings.ReadTimeout, WriteTimeout: settings.WriteTimeout, IdleTimeout: settings.IdleTimeout}
}
