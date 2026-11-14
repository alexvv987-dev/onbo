package config

import (
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	ModeDemo     = "demo"
	ModeGigaChat = "gigachat"
	maxPort      = 65535
)

func validateAddress(field, address string) error {
	_, port, err := net.SplitHostPort(address)
	if err != nil {
		return invalidField(field)
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil || portNumber < 0 || portNumber > maxPort {
		return invalidField(field)
	}
	return nil
}

func validateDurations(durations map[string]time.Duration) error {
	for field, duration := range durations {
		if duration <= 0 {
			return invalidField(field)
		}
	}
	return nil
}

func validateHTTP(settings HTTP, lifecycle Lifecycle) error {
	if err := validateAddress("http.address", settings.Address); err != nil {
		return err
	}
	return validateDurations(map[string]time.Duration{"http.read_header_timeout": settings.ReadHeaderTimeout, "http.read_timeout": settings.ReadTimeout, "http.write_timeout": settings.WriteTimeout, "http.idle_timeout": settings.IdleTimeout, "lifecycle.init_timeout": lifecycle.InitTimeout, "lifecycle.shutdown_timeout": lifecycle.ShutdownTimeout})
}

func validateDatabase(databaseURL string) error {
	if strings.TrimSpace(databaseURL) == "" {
		return invalidField(envDatabaseURL)
	}
	if _, err := pgxpool.ParseConfig(databaseURL); err != nil {
		return invalidField(envDatabaseURL)
	}
	return nil
}

func validateURL(field, address string, provider bool) error {
	parsedURL, err := url.Parse(address)
	if err != nil || parsedURL.Hostname() == "" || parsedURL.User != nil || parsedURL.RawQuery != "" || parsedURL.Fragment != "" {
		return invalidField(field)
	}
	if port := parsedURL.Port(); port != "" {
		portNumber, err := strconv.Atoi(port)
		if err != nil || portNumber <= 0 || portNumber > maxPort {
			return invalidField(field)
		}
	}
	if parsedURL.Scheme == "https" {
		return nil
	}
	if parsedURL.Scheme == "http" && (!provider || parsedURL.Hostname() == "localhost" || parsedURL.Hostname() == "127.0.0.1" || parsedURL.Hostname() == "::1") {
		return nil
	}
	return invalidField(field)
}

func (settings Agent) Validate() error {
	mode := strings.ToLower(strings.TrimSpace(settings.Mode))
	if mode != ModeDemo && mode != ModeGigaChat {
		return invalidField(envLLMMode)
	}
	if err := validateDurations(map[string]time.Duration{"llm.request_timeout": settings.RequestTimeout, "llm.reply_timeout": settings.ReplyTimeout}); err != nil {
		return err
	}
	if mode == ModeDemo {
		return nil
	}
	if strings.TrimSpace(settings.AuthorizationKey) == "" {
		return invalidField(envAuthorizationKey)
	}
	if strings.TrimSpace(settings.Model) == "" {
		return invalidField(envModel)
	}
	if strings.TrimSpace(settings.Scope) == "" {
		return invalidField(envScope)
	}
	if err := validateURL(envAPIURL, settings.APIURL, true); err != nil {
		return err
	}
	return validateURL(envOAuthURL, settings.OAuthURL, true)
}

func (settings Config) Validate() error {
	if err := validateHTTP(settings.HTTP, settings.Lifecycle); err != nil {
		return err
	}
	if err := validateDatabase(settings.DatabaseURL); err != nil {
		return err
	}
	if err := validateAddress(envRedisAddress, settings.Redis.Address); err != nil {
		return err
	}
	if err := validateURL(envMockURL, settings.Mock.URL, false); err != nil {
		return err
	}
	if strings.TrimSpace(settings.Mock.Token) == "" {
		return invalidField(envMockToken)
	}
	if err := validateDurations(map[string]time.Duration{"redis.dial_timeout": settings.Redis.DialTimeout, "redis.read_timeout": settings.Redis.ReadTimeout, "redis.write_timeout": settings.Redis.WriteTimeout, "redis.session_ttl": settings.Redis.SessionTTL, "redis.rate_window": settings.Redis.RateWindow, "mock.timeout": settings.Mock.Timeout, "api.health_timeout": settings.API.HealthTimeout, "api.chat_timeout": settings.API.ChatTimeout}); err != nil {
		return err
	}
	if settings.Redis.SessionTTL%time.Second != 0 {
		return invalidField("redis.session_ttl")
	}
	if settings.Redis.ChatRateLimit <= 0 {
		return invalidField("redis.chat_rate_limit")
	}
	if settings.API.MaxRequestBytes <= 0 {
		return invalidField("api.max_request_bytes")
	}
	return settings.Agent.Validate()
}

func (settings Mock) Validate() error {
	if err := validateHTTP(HTTP(settings.HTTP), settings.Lifecycle); err != nil {
		return err
	}
	if err := validateDatabase(settings.DatabaseURL); err != nil {
		return err
	}
	if strings.TrimSpace(settings.Token) == "" {
		return invalidField(envMockToken)
	}
	if settings.MaxRequestBytes <= 0 {
		return invalidField("max_request_bytes")
	}
	return validateDurations(map[string]time.Duration{"health_timeout": settings.HealthTimeout})
}
