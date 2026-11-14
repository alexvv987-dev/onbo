package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/ilyakaznacheev/cleanenv"
	"go.yaml.in/yaml/v3"
)

const (
	DefaultAppPath           = "configs/onbo.yaml"
	DefaultMockPath          = "configs/mock.yaml"
	MaxConfigBytes           = 64 << 10
	envConfigFile            = "CONFIG_FILE"
	envDatabaseURL           = "DATABASE_URL"
	envRedisAddress          = "REDIS_ADDR"
	envMockURL               = "MOCK_URL"
	envMockToken             = "MOCK_TOKEN"
	envLLMMode               = "LLM_MODE"
	envAuthorizationKey      = "GIGACHAT_AUTHORIZATION_KEY"
	envModel                 = "GIGACHAT_MODEL"
	envScope                 = "GIGACHAT_SCOPE"
	envAPIURL                = "GIGACHAT_API_URL"
	envOAuthURL              = "GIGACHAT_OAUTH_URL"
	configurationErrorFormat = "invalid configuration field %s"
)

func invalidField(field string) error { return fmt.Errorf(configurationErrorFormat, field) }

// readYAML validates the exact schema before cleanenv is allowed to decode it.
func readYAML(defaultPath, explicitPath string, settings any) (string, error) {
	configPath := explicitPath
	if configPath == "" {
		configPath = os.Getenv(envConfigFile)
	}
	pathIsExplicit := configPath != ""
	if !pathIsExplicit {
		configPath = defaultPath
	}
	configFile, err := os.Open(configPath)
	if err != nil {
		if !pathIsExplicit && errors.Is(err, os.ErrNotExist) {
			return "", nil
		}
		return "", invalidField(envConfigFile)
	}
	defer configFile.Close()
	contents, err := io.ReadAll(io.LimitReader(configFile, MaxConfigBytes+1))
	if err != nil || len(contents) > MaxConfigBytes {
		return "", invalidField(envConfigFile)
	}
	decoder := yaml.NewDecoder(bytes.NewReader(contents))
	var document yaml.Node
	if err = decoder.Decode(&document); err != nil || len(document.Content) != 1 {
		return "", invalidField(envConfigFile)
	}
	if err = validateYAMLTypes(document.Content[0], reflect.TypeOf(settings).Elem(), ""); err != nil {
		return "", err
	}
	var trailingDocument any
	if err = decoder.Decode(&trailingDocument); !errors.Is(err, io.EOF) {
		return "", invalidField(envConfigFile)
	}
	// This first decode captures explicit zero values before cleanenv applies tags.
	if err = cleanenv.ParseYAML(bytes.NewReader(contents), settings); err != nil {
		return "", invalidField(envConfigFile)
	}
	return configPath, nil
}

func validateTaggedEnvironment(settings reflect.Value) error {
	for fieldIndex := 0; fieldIndex < settings.NumField(); fieldIndex++ {
		field := settings.Type().Field(fieldIndex)
		value := settings.Field(fieldIndex)
		if value.Kind() == reflect.Struct {
			if err := validateTaggedEnvironment(value); err != nil {
				return err
			}
			continue
		}
		name := field.Tag.Get("env")
		if name == "" {
			continue
		}
		rawValue, exists := os.LookupEnv(name)
		if !exists {
			if field.Tag.Get("env-required") == "true" && value.IsZero() {
				return invalidField(name)
			}
			continue
		}
		var err error
		if value.Type() == reflect.TypeOf(time.Duration(0)) {
			_, err = time.ParseDuration(rawValue)
		} else {
			switch value.Kind() {
			case reflect.Bool:
				_, err = strconv.ParseBool(rawValue)
			case reflect.Int, reflect.Int64:
				var decimalValue int64
				decimalValue, err = strconv.ParseInt(rawValue, 10, value.Type().Bits())
				if err == nil && strconv.FormatInt(decimalValue, 10) != rawValue {
					return invalidField(name)
				}
			}
		}
		if err != nil {
			return invalidField(name)
		}
	}
	return nil
}

// Explicit empty YAML values must not be replaced by cleanenv's zero-value defaults.
func restoreUnspecifiedZeroValues(target, snapshot reflect.Value) {
	for fieldIndex := 0; fieldIndex < target.NumField(); fieldIndex++ {
		field := target.Field(fieldIndex)
		previous := snapshot.Field(fieldIndex)
		if field.Kind() == reflect.Struct {
			restoreUnspecifiedZeroValues(field, previous)
			continue
		}
		name := target.Type().Field(fieldIndex).Tag.Get("env")
		if _, exists := os.LookupEnv(name); !exists && previous.IsZero() {
			field.Set(previous)
		}
	}
}

func readEnvironment(settings any) error {
	value := reflect.ValueOf(settings).Elem()
	if err := validateTaggedEnvironment(value); err != nil {
		return err
	}
	snapshot := reflect.New(value.Type()).Elem()
	snapshot.Set(value)
	err := cleanenv.ReadEnv(settings)
	if err != nil {
		return invalidField(envConfigFile)
	}
	restoreUnspecifiedZeroValues(value, snapshot)
	return nil
}

func Load(configPath string) (Config, error) {
	settings := DefaultApp()
	_, err := readYAML(DefaultAppPath, configPath, &settings)
	if err != nil {
		return Config{}, err
	}
	if err = readEnvironment(&settings); err != nil {
		return Config{}, err
	}
	settings.Agent.Mode = strings.ToLower(strings.TrimSpace(settings.Agent.Mode))
	if err = settings.Validate(); err != nil {
		return Config{}, err
	}
	return settings, nil
}

func LoadApp() (Config, error) { return Load("") }

func LoadMockFile(configPath string) (Mock, error) {
	settings := DefaultMock()
	_, err := readYAML(DefaultMockPath, configPath, &settings)
	if err != nil {
		return Mock{}, err
	}
	if err = readEnvironment(&settings); err != nil {
		return Mock{}, err
	}
	if err = settings.Validate(); err != nil {
		return Mock{}, err
	}
	return settings, nil
}

func LoadMock() (Mock, error) { return LoadMockFile("") }

func LoadAgent() (Agent, error) {
	settings := DefaultApp()
	if _, err := readYAML(DefaultAppPath, "", &settings); err != nil {
		return Agent{}, err
	}
	// Eval intentionally applies only provider tags, never app-required DB/session tags.
	if err := readEnvironment(&settings.Agent); err != nil {
		return Agent{}, err
	}
	settings.Agent.Mode = strings.ToLower(strings.TrimSpace(settings.Agent.Mode))
	if err := settings.Agent.Validate(); err != nil {
		return Agent{}, err
	}
	return settings.Agent, nil
}
