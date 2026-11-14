package agent

import (
	"net/http"
	"onbo/internal/config"
	"onbo/internal/core"
	"strings"
	"time"
)

// Config is the provider adapter configuration, including an optional test client.
type Config struct {
	AuthorizationKey string
	Model            string
	APIURL           string
	OAuthURL         string
	Scope            string
	CAFile           string
	HTTPClient       *http.Client
	RequestTimeout   time.Duration
	ReplyTimeout     time.Duration
}

func NewFromEnv(tools core.Tools) (core.ChatAgent, error) {
	settings, err := config.LoadAgent()
	if err != nil {
		return nil, err
	}
	return NewFromConfig(settings, tools)
}

func NewFromConfig(settings config.Agent, tools core.Tools) (core.ChatAgent, error) {
	if err := settings.Validate(); err != nil {
		return nil, err
	}
	if strings.ToLower(strings.TrimSpace(settings.Mode)) == config.ModeDemo {
		return NewDemo(tools), nil
	}
	return New(Config{AuthorizationKey: settings.AuthorizationKey, Model: settings.Model, APIURL: settings.APIURL, OAuthURL: settings.OAuthURL, Scope: settings.Scope, CAFile: settings.CAFile, RequestTimeout: settings.RequestTimeout, ReplyTimeout: settings.ReplyTimeout}, tools)
}
