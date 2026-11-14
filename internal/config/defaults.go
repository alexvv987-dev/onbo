package config

import (
	"reflect"
	"strconv"
	"time"
)

const (
	defaultValueTag = "env-default"
)

// Defaults come from tags only and never depend on the process environment.
func applyTagDefaults(settings reflect.Value) error {
	for fieldIndex := 0; fieldIndex < settings.NumField(); fieldIndex++ {
		field := settings.Field(fieldIndex)
		declaration := settings.Type().Field(fieldIndex)
		if field.Kind() == reflect.Struct {
			if err := applyTagDefaults(field); err != nil {
				return err
			}
			continue
		}
		value, exists := declaration.Tag.Lookup(defaultValueTag)
		if !exists {
			continue
		}
		var err error
		if field.Type() == reflect.TypeOf(time.Duration(0)) {
			var duration time.Duration
			duration, err = time.ParseDuration(value)
			field.SetInt(int64(duration))
		} else {
			switch field.Kind() {
			case reflect.String:
				field.SetString(value)
			case reflect.Bool:
				var parsedValue bool
				parsedValue, err = strconv.ParseBool(value)
				field.SetBool(parsedValue)
			case reflect.Int, reflect.Int64:
				var parsedValue int64
				parsedValue, err = strconv.ParseInt(value, 10, field.Type().Bits())
				field.SetInt(parsedValue)
			default:
				return invalidField(declaration.Name)
			}
		}
		if err != nil {
			return invalidField(declaration.Name)
		}
	}
	return nil
}

func defaultSettings[Settings any]() Settings {
	var settings Settings
	if err := applyTagDefaults(reflect.ValueOf(&settings).Elem()); err != nil {
		panic(err)
	}
	return settings
}

func DefaultLifecycle() Lifecycle { return defaultSettings[Lifecycle]() }
func DefaultRedis() Redis         { return defaultSettings[Redis]() }
func DefaultAgent() Agent         { return defaultSettings[Agent]() }
func DefaultApp() Config          { return defaultSettings[Config]() }
func DefaultMock() Mock           { return defaultSettings[Mock]() }
