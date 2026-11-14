package config

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func prepareEnvironment(test *testing.T) {
	test.Helper()
	test.Chdir(test.TempDir())
	environmentNames := []string{"CONFIG_FILE", "DATABASE_URL", "REDIS_ADDR", "REDIS_PASSWORD", "MOCK_URL", "MOCK_TOKEN", "DEMO_MODE", "STATIC_DIR", "HTTP_ADDR", "HTTP_READ_HEADER_TIMEOUT", "HTTP_READ_TIMEOUT", "HTTP_WRITE_TIMEOUT", "HTTP_IDLE_TIMEOUT", "INIT_TIMEOUT", "SHUTDOWN_TIMEOUT", "REDIS_DIAL_TIMEOUT", "REDIS_READ_TIMEOUT", "REDIS_WRITE_TIMEOUT", "SESSION_TTL", "CHAT_RATE_WINDOW", "CHAT_RATE_LIMIT", "MOCK_TIMEOUT", "HEALTH_TIMEOUT", "CHAT_TIMEOUT", "MAX_REQUEST_BYTES", "LLM_MODE", "GIGACHAT_AUTHORIZATION_KEY", "GIGACHAT_MODEL", "GIGACHAT_API_URL", "GIGACHAT_OAUTH_URL", "GIGACHAT_SCOPE", "GIGACHAT_CA_FILE", "GIGACHAT_REQUEST_TIMEOUT", "GIGACHAT_REPLY_TIMEOUT"}
	for _, name := range environmentNames {
		test.Setenv(name, "")
		if err := os.Unsetenv(name); err != nil {
			test.Fatal(err)
		}
	}
	test.Setenv("DATABASE_URL", "postgres://test:example@localhost:5432/onbo?sslmode=disable")
	test.Setenv("REDIS_ADDR", "localhost:6379")
	test.Setenv("MOCK_TOKEN", "test-only-token")
	test.Setenv("LLM_MODE", "demo")
}

func TestTagDefaultsAndIntegerSyntax(test *testing.T) {
	prepareEnvironment(test)
	test.Setenv("HTTP_ADDR", ":9999")
	if DefaultApp().HTTP.Address != ":8080" || DefaultMock().HTTP.Address != ":8081" {
		test.Fatal("defaults depend on environment")
	}
	type customDefaults struct {
		Timeout time.Duration `env-default:"7s"`
		Limit   int           `env-default:"3"`
		Enabled bool          `env-default:"true"`
	}
	settings := defaultSettings[customDefaults]()
	if settings.Timeout != 7*time.Second || settings.Limit != 3 || !settings.Enabled {
		test.Fatal("default tags ignored")
	}
	for _, invalid := range []any{
		&struct {
			Count int `env-default:"bad"`
		}{},
		&struct {
			Enabled bool `env-default:"bad"`
		}{},
		&struct {
			Timeout time.Duration `env-default:"bad"`
		}{},
		&struct {
			Unsupported float64 `env-default:"1"`
		}{},
	} {
		if err := applyTagDefaults(reflect.ValueOf(invalid).Elem()); err == nil {
			test.Fatal("invalid default accepted")
		}
	}
	for _, value := range []string{"010", "08", "+10"} {
		test.Setenv("CHAT_RATE_LIMIT", value)
		if _, err := LoadApp(); err == nil || !strings.Contains(err.Error(), "CHAT_RATE_LIMIT") {
			test.Fatal("ambiguous integer syntax accepted")
		}
	}
}

func TestYAMLSnapshotAndExtensionlessFile(test *testing.T) {
	prepareEnvironment(test)
	configPath := writeConfig(test, "http:\n  address: ':9090'")
	settings := DefaultApp()
	if _, err := readYAML(DefaultAppPath, configPath, &settings); err != nil {
		test.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte("unknown: control-secret"), 0600); err != nil {
		test.Fatal(err)
	}
	if err := readEnvironment(&settings); err != nil || settings.HTTP.Address != ":9090" {
		test.Fatal("configuration snapshot reread")
	}
	noExtension := filepath.Join(test.TempDir(), "service-config")
	if err := os.WriteFile(noExtension, []byte("{}"), 0600); err != nil {
		test.Fatal(err)
	}
	if _, err := Load(noExtension); err != nil {
		test.Fatal(err)
	}
}

func TestMustLoadWrappers(test *testing.T) {
	prepareEnvironment(test)
	configPath := writeConfig(test, "{}")
	if settings := MustLoad(configPath); settings.Agent.Mode != "demo" {
		test.Fatal("application wrapper ignored settings")
	}
	if settings := MustLoadMock(configPath); settings.HTTP.Address != ":8081" {
		test.Fatal("mock wrapper ignored defaults")
	}
	for _, service := range []string{"app", "mock"} {
		command := exec.Command(os.Args[0], "-test.run=^TestMustLoadFailure$")
		command.Env = append(os.Environ(), "ONBO_TEST_FATAL_CONFIG="+service)
		output, err := command.CombinedOutput()
		if err == nil || !strings.Contains(string(output), "cannot read config") || strings.Contains(string(output), "control-secret") {
			test.Fatalf("fail-fast wrapper: %v", err)
		}
	}
}

func TestMustLoadFailure(test *testing.T) {
	switch os.Getenv("ONBO_TEST_FATAL_CONFIG") {
	case "app":
		MustLoad("control-secret-does-not-exist.yaml")
	case "mock":
		MustLoadMock("control-secret-does-not-exist.yaml")
	}
}

func TestExplicitYAMLZeroAndDiagnostics(test *testing.T) {
	prepareEnvironment(test)
	writeConfig(test, "static_dir: ''\ndemo_mode: false")
	settings, err := LoadApp()
	if err != nil || settings.StaticDir != "" || settings.DemoMode {
		test.Fatalf("explicit YAML zero changed: %v", err)
	}
	writeConfig(test, "http:\n  read_timeout: control-secret")
	_, err = LoadApp()
	if err == nil || !strings.Contains(err.Error(), "http.read_timeout") || strings.Contains(err.Error(), "control-secret") {
		test.Fatal("unsafe or unhelpful field diagnostics")
	}
	writeConfig(test, "api:\n  chat_timeout: 0s\nredis:\n  chat_rate_limit: 0")
	test.Setenv("CHAT_TIMEOUT", "2s")
	test.Setenv("CHAT_RATE_LIMIT", "3")
	settings, err = LoadApp()
	if err != nil || settings.API.ChatTimeout != 2*time.Second || settings.Redis.ChatRateLimit != 3 {
		test.Fatalf("final semantic overrides ignored: %v", err)
	}
}

func writeConfig(test *testing.T, contents string) string {
	test.Helper()
	configPath := filepath.Join(test.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte(contents), 0600); err != nil {
		test.Fatal(err)
	}
	test.Setenv("CONFIG_FILE", configPath)
	return configPath
}

func TestDefaultsYAMLAndEnvironment(test *testing.T) {
	prepareEnvironment(test)
	settings, err := LoadApp()
	if err != nil || settings.HTTP.Address != ":8080" || settings.Redis.SessionTTL != 12*time.Hour || settings.DemoMode {
		test.Fatalf("defaults: %v", err)
	}
	writeConfig(test, "http:\n  address: ':9000'\n  read_timeout: 7s\ndemo_mode: true\nredis:\n  session_ttl: 30m\n")
	test.Setenv("HTTP_ADDR", ":9001")
	test.Setenv("DEMO_MODE", "false")
	test.Setenv("STATIC_DIR", "")
	settings, err = LoadApp()
	if err != nil {
		test.Fatal(err)
	}
	if settings.HTTP.Address != ":9001" || settings.HTTP.ReadTimeout != 7*time.Second || settings.DemoMode || settings.StaticDir != "" || settings.Redis.SessionTTL != 30*time.Minute {
		test.Fatal("precedence or units changed")
	}
	test.Setenv("LLM_MODE", " DEMO ")
	test.Setenv("CHAT_RATE_WINDOW", "1.5s")
	settings, err = LoadApp()
	if err != nil || settings.Agent.Mode != "demo" || settings.Redis.RateWindow != 1500*time.Millisecond {
		test.Fatalf("normalization and fractional window: %v", err)
	}
	test.Setenv("CHAT_RATE_LIMIT", "8")
	test.Setenv("MAX_REQUEST_BYTES", "1024")
	test.Setenv("CHAT_TIMEOUT", "2.5s")
	settings, err = LoadApp()
	if err != nil || settings.Redis.ChatRateLimit != 8 || settings.API.MaxRequestBytes != 1024 || settings.API.ChatTimeout != 2500*time.Millisecond {
		test.Fatalf("typed environment: %v", err)
	}
}

func TestStrictYAML(test *testing.T) {
	for _, contents := range []string{
		"unknown: true", "http:\n  address: ':1'\n  address: ':2'", "http: [", "demo_mode: perhaps",
		"null", "http: null", "http:\n  read_timeout: null", "[]", "",
		"static_dir: true", "llm:\n  model: 123", "demo_mode: 'false'", "api:\n  max_request_bytes: '123'",
		"redis:\n  chat_rate_limit: 2.9", "api:\n  max_request_bytes: 1024.9",
		"http:\n  read_timeout: 12", "http:\n  read_timeout: secret-invalid-duration",
		"http: {}\n---\nhttp: {}", "redis:\n  password: control-secret", "llm:\n  authorization_key: control-secret", "database_url: control-secret",
		strings.Repeat("# oversized\n", MaxConfigBytes),
	} {
		test.Run("invalid", func(test *testing.T) {
			prepareEnvironment(test)
			writeConfig(test, contents)
			_, err := LoadApp()
			if err == nil || strings.Contains(err.Error(), "control-secret") || strings.Contains(err.Error(), "secret-invalid") {
				test.Fatal("invalid YAML accepted or sensitive value leaked")
			}
		})
	}
}

func TestFileSelection(test *testing.T) {
	prepareEnvironment(test)
	test.Setenv("CONFIG_FILE", "absent.yaml")
	if _, err := LoadApp(); err == nil {
		test.Fatal("explicit missing file accepted")
	}
	test.Setenv("CONFIG_FILE", "")
	if _, err := LoadMock(); err != nil {
		test.Fatal(err)
	}
	if err := os.MkdirAll("configs", 0700); err != nil {
		test.Fatal(err)
	}
	if err := os.WriteFile(DefaultAppPath, []byte("broken: true"), 0600); err != nil {
		test.Fatal(err)
	}
	if _, err := LoadApp(); err == nil {
		test.Fatal("broken default file ignored")
	}
	test.Setenv("CONFIG_FILE", ".")
	if _, err := LoadMock(); err == nil {
		test.Fatal("directory accepted as config")
	}
}

func TestInvalidConfigurationAndRedaction(test *testing.T) {
	invalidEnvironment := map[string]string{
		"DATABASE_URL": "postgres://user:control-secret@host:invalid/db", "REDIS_ADDR": "wrong", "MOCK_URL": "ftp://host",
		"MOCK_TOKEN": "", "HTTP_ADDR": ":99999", "DEMO_MODE": "", "CHAT_RATE_LIMIT": "zero", "MAX_REQUEST_BYTES": "bad",
		"CHAT_TIMEOUT": "bad", "INIT_TIMEOUT": "-1s", "SESSION_TTL": "500ms",
		"REDIS_READ_TIMEOUT": "0s", "CHAT_RATE_LIMIT#negative": "-1", "MAX_REQUEST_BYTES#negative": "0",
	}
	for name, value := range invalidEnvironment {
		test.Run(name, func(test *testing.T) {
			prepareEnvironment(test)
			test.Setenv(strings.Split(name, "#")[0], value)
			_, err := LoadApp()
			if err == nil || strings.Contains(err.Error(), "control-secret") {
				test.Fatal("invalid setting accepted or secret leaked")
			}
		})
	}
}

func TestAgentIsolationAndModes(test *testing.T) {
	prepareEnvironment(test)
	test.Setenv("DATABASE_URL", "invalid-unrelated-database")
	test.Setenv("REDIS_ADDR", "invalid-unrelated-redis")
	test.Setenv("LLM_MODE", "gigachat")
	test.Setenv("GIGACHAT_AUTHORIZATION_KEY", "control-secret")
	test.Setenv("GIGACHAT_MODEL", "test-model")
	settings, err := LoadAgent()
	if err != nil || settings.Model != "test-model" {
		test.Fatalf("eval requires unrelated dependencies: %v", err)
	}
	encodedSettings, err := json.Marshal(settings)
	if err != nil || strings.Contains(string(encodedSettings), "control-secret") {
		test.Fatal("secret serialized")
	}
	for name, value := range map[string]string{"LLM_MODE": "unknown", "GIGACHAT_AUTHORIZATION_KEY": "", "GIGACHAT_MODEL": "", "GIGACHAT_SCOPE": "", "GIGACHAT_API_URL": "http://external.example", "GIGACHAT_OAUTH_URL": "https://user:secret@example.com", "GIGACHAT_REPLY_TIMEOUT": "0s"} {
		test.Run(name, func(test *testing.T) {
			test.Setenv(name, value)
			if _, err := LoadAgent(); err == nil {
				test.Fatal("invalid agent accepted")
			}
		})
	}
	for _, endpoint := range []string{"https://example.com?secret=x", ":bad", "https://example.com#fragment", "https://:443", "https://example.com:99999", "", "http://localhost:1234", "http://127.0.0.1:1234", "http://[::1]:1234"} {
		test.Setenv("GIGACHAT_API_URL", endpoint)
		_, err := LoadAgent()
		if strings.HasPrefix(endpoint, "http://") && err != nil {
			test.Fatal("local HTTP rejected")
		}
		if !strings.HasPrefix(endpoint, "http://") && err == nil {
			test.Fatal("invalid endpoint accepted")
		}
	}
	test.Setenv("LLM_MODE", "demo")
	test.Setenv("GIGACHAT_AUTHORIZATION_KEY", "")
	test.Setenv("GIGACHAT_MODEL", "")
	if _, err := LoadAgent(); err != nil {
		test.Fatal(err)
	}
}

func TestSuppliedFilesAndMockValidation(test *testing.T) {
	projectDirectory, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		test.Fatal(err)
	}
	prepareEnvironment(test)
	test.Setenv("CONFIG_FILE", filepath.Join(projectDirectory, DefaultAppPath))
	if _, err := LoadApp(); err != nil {
		test.Fatal(err)
	}
	test.Setenv("CONFIG_FILE", filepath.Join(projectDirectory, DefaultMockPath))
	settings, err := LoadMock()
	if err != nil || settings.HTTP.Address != ":8081" {
		test.Fatal(err)
	}
	for name, value := range map[string]string{"DATABASE_URL": "", "MOCK_TOKEN": "", "HTTP_ADDR": "invalid", "MAX_REQUEST_BYTES": "0", "HEALTH_TIMEOUT": "0s", "HTTP_WRITE_TIMEOUT": "oops"} {
		test.Run(name, func(test *testing.T) {
			test.Setenv(name, value)
			if _, err := LoadMock(); err == nil {
				test.Fatal("invalid mock accepted")
			}
		})
	}
}
