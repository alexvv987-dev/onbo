// Package config loads validated service settings without opening network connections.
package config

import (
	"log"
	"time"
)

const (
	mustLoadErrorFormat = "cannot read config: %s"
)

type HTTP struct {
	Address           string        `yaml:"address" env:"HTTP_ADDR" env-default:":8080"`
	ReadHeaderTimeout time.Duration `yaml:"read_header_timeout" env:"HTTP_READ_HEADER_TIMEOUT" env-default:"5s"`
	ReadTimeout       time.Duration `yaml:"read_timeout" env:"HTTP_READ_TIMEOUT" env-default:"10s"`
	WriteTimeout      time.Duration `yaml:"write_timeout" env:"HTTP_WRITE_TIMEOUT" env-default:"60s"`
	IdleTimeout       time.Duration `yaml:"idle_timeout" env:"HTTP_IDLE_TIMEOUT" env-default:"60s"`
}

type MockHTTP struct {
	Address           string        `yaml:"address" env:"HTTP_ADDR" env-default:":8081"`
	ReadHeaderTimeout time.Duration `yaml:"read_header_timeout" env:"HTTP_READ_HEADER_TIMEOUT" env-default:"5s"`
	ReadTimeout       time.Duration `yaml:"read_timeout" env:"HTTP_READ_TIMEOUT" env-default:"10s"`
	WriteTimeout      time.Duration `yaml:"write_timeout" env:"HTTP_WRITE_TIMEOUT" env-default:"10s"`
	IdleTimeout       time.Duration `yaml:"idle_timeout" env:"HTTP_IDLE_TIMEOUT" env-default:"60s"`
}

type Lifecycle struct {
	InitTimeout     time.Duration `yaml:"init_timeout" env:"INIT_TIMEOUT" env-default:"15s"`
	ShutdownTimeout time.Duration `yaml:"shutdown_timeout" env:"SHUTDOWN_TIMEOUT" env-default:"10s"`
}

type Redis struct {
	Address       string        `yaml:"address" env:"REDIS_ADDR" env-required:"true"`
	Password      string        `yaml:"-" env:"REDIS_PASSWORD" json:"-"`
	DialTimeout   time.Duration `yaml:"dial_timeout" env:"REDIS_DIAL_TIMEOUT" env-default:"3s"`
	ReadTimeout   time.Duration `yaml:"read_timeout" env:"REDIS_READ_TIMEOUT" env-default:"3s"`
	WriteTimeout  time.Duration `yaml:"write_timeout" env:"REDIS_WRITE_TIMEOUT" env-default:"3s"`
	SessionTTL    time.Duration `yaml:"session_ttl" env:"SESSION_TTL" env-default:"12h"`
	RateWindow    time.Duration `yaml:"rate_window" env:"CHAT_RATE_WINDOW" env-default:"60s"`
	ChatRateLimit int           `yaml:"chat_rate_limit" env:"CHAT_RATE_LIMIT" env-default:"20"`
}

type AccessService struct {
	URL     string        `yaml:"url" env:"MOCK_URL" env-default:"http://mock:8081"`
	Token   string        `yaml:"-" env:"MOCK_TOKEN" env-required:"true" json:"-"`
	Timeout time.Duration `yaml:"timeout" env:"MOCK_TIMEOUT" env-default:"4s"`
}

type API struct {
	HealthTimeout   time.Duration `yaml:"health_timeout" env:"HEALTH_TIMEOUT" env-default:"2s"`
	ChatTimeout     time.Duration `yaml:"chat_timeout" env:"CHAT_TIMEOUT" env-default:"45s"`
	MaxRequestBytes int64         `yaml:"max_request_bytes" env:"MAX_REQUEST_BYTES" env-default:"32768"`
}

type Agent struct {
	Mode             string        `yaml:"mode" env:"LLM_MODE" env-default:"gigachat"`
	AuthorizationKey string        `yaml:"-" env:"GIGACHAT_AUTHORIZATION_KEY" json:"-"`
	Model            string        `yaml:"model" env:"GIGACHAT_MODEL"`
	APIURL           string        `yaml:"api_url" env:"GIGACHAT_API_URL" env-default:"https://api.giga.chat/v1"`
	OAuthURL         string        `yaml:"oauth_url" env:"GIGACHAT_OAUTH_URL" env-default:"https://ngw.devices.sberbank.ru:9443/api/v2/oauth"`
	Scope            string        `yaml:"scope" env:"GIGACHAT_SCOPE" env-default:"GIGACHAT_API_PERS"`
	CAFile           string        `yaml:"ca_file" env:"GIGACHAT_CA_FILE"`
	RequestTimeout   time.Duration `yaml:"request_timeout" env:"GIGACHAT_REQUEST_TIMEOUT" env-default:"45s"`
	ReplyTimeout     time.Duration `yaml:"reply_timeout" env:"GIGACHAT_REPLY_TIMEOUT" env-default:"90s"`
}

type Config struct {
	HTTP        HTTP          `yaml:"http"`
	Lifecycle   Lifecycle     `yaml:"lifecycle"`
	Redis       Redis         `yaml:"redis"`
	Mock        AccessService `yaml:"mock"`
	Agent       Agent         `yaml:"llm"`
	API         API           `yaml:"api"`
	DemoMode    bool          `yaml:"demo_mode" env:"DEMO_MODE" env-default:"false"`
	StaticDir   string        `yaml:"static_dir" env:"STATIC_DIR" env-default:"web/dist"`
	DatabaseURL string        `yaml:"-" env:"DATABASE_URL" env-required:"true" json:"-"`
}

type Mock struct {
	HTTP            MockHTTP      `yaml:"http"`
	Lifecycle       Lifecycle     `yaml:"lifecycle"`
	HealthTimeout   time.Duration `yaml:"health_timeout" env:"HEALTH_TIMEOUT" env-default:"1s"`
	MaxRequestBytes int64         `yaml:"max_request_bytes" env:"MAX_REQUEST_BYTES" env-default:"16384"`
	DatabaseURL     string        `yaml:"-" env:"DATABASE_URL" env-required:"true" json:"-"`
	Token           string        `yaml:"-" env:"MOCK_TOKEN" env-required:"true" json:"-"`
}

// MustLoad is the fail-fast application entry point; Load returns errors for tests and controlled shutdown.
func MustLoad(configPath string) Config {
	settings, err := Load(configPath)
	if err != nil {
		log.Fatalf(mustLoadErrorFormat, err)
	}
	return settings
}

func MustLoadMock(configPath string) Mock {
	settings, err := LoadMockFile(configPath)
	if err != nil {
		log.Fatalf(mustLoadErrorFormat, err)
	}
	return settings
}
