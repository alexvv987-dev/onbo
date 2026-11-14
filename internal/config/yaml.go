package config

import (
	"reflect"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"
)

const (
	yamlStringTag      = "!!str"
	yamlBooleanTag     = "!!bool"
	yamlIntegerTag     = "!!int"
	yamlFieldSeparator = "."
)

func validateYAMLTypes(node *yaml.Node, expected reflect.Type, fieldPath string) error {
	errorPath := fieldPath
	if errorPath == "" {
		errorPath = envConfigFile
	}
	if node.Kind == yaml.AliasNode {
		return validateYAMLTypes(node.Alias, expected, fieldPath)
	}
	if expected == reflect.TypeOf(time.Duration(0)) {
		if node.Kind != yaml.ScalarNode || node.Tag != yamlStringTag {
			return invalidField(errorPath)
		}
		_, err := time.ParseDuration(node.Value)
		if err != nil {
			return invalidField(errorPath)
		}
		return nil
	}
	switch expected.Kind() {
	case reflect.Struct:
		if node.Kind != yaml.MappingNode {
			return invalidField(errorPath)
		}
		fields := map[string]reflect.Type{}
		for fieldIndex := 0; fieldIndex < expected.NumField(); fieldIndex++ {
			field := expected.Field(fieldIndex)
			name := strings.Split(field.Tag.Get("yaml"), ",")[0]
			if name != "-" && name != "" {
				fields[name] = field.Type
			}
		}
		seen := map[string]bool{}
		for childIndex := 0; childIndex < len(node.Content); childIndex += 2 {
			key := node.Content[childIndex]
			fieldType, exists := fields[key.Value]
			if key.Tag != yamlStringTag || !exists {
				return invalidField(errorPath)
			}
			childPath := key.Value
			if fieldPath != "" {
				childPath = fieldPath + yamlFieldSeparator + childPath
			}
			if seen[key.Value] {
				return invalidField(childPath)
			}
			seen[key.Value] = true
			if err := validateYAMLTypes(node.Content[childIndex+1], fieldType, childPath); err != nil {
				return err
			}
		}
		return nil
	case reflect.String:
		if node.Kind == yaml.ScalarNode && node.Tag == yamlStringTag {
			return nil
		}
	case reflect.Bool:
		if node.Kind == yaml.ScalarNode && node.Tag == yamlBooleanTag {
			return nil
		}
	case reflect.Int, reflect.Int64:
		if node.Kind == yaml.ScalarNode && node.Tag == yamlIntegerTag {
			var parsedInteger int64
			if err := node.Decode(&parsedInteger); err == nil {
				return nil
			}
		}
	}
	return invalidField(errorPath)
}
