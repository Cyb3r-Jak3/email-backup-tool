package cmd

import (
	"fmt"
	"maps"
	"reflect"
	"strconv"
	"strings"
)

// schemaOverrides adds cross-field constraints that don't fall out of a single
// struct's fields, keyed by the dotted yaml path of the object they attach to
// (the empty string is the document root). These are the only parts of
// config.schema.json a struct change won't update automatically, so keep this
// map small and re-check it whenever ConfigV1's shape changes.
var schemaOverrides = map[string]map[string]any{
	"save": {
		"minProperties": 1,
	},
	"encrypt": {
		// At most one of the three key sources may be set.
		"not": map[string]any{
			"anyOf": []any{
				map[string]any{"required": []any{"public_key_file", "public_key_string"}},
				map[string]any{"required": []any{"public_key_file", "public_key_fingerprint"}},
				map[string]any{"required": []any{"public_key_string", "public_key_fingerprint"}},
			},
		},
	},
}

// GenerateConfigSchema builds the JSON Schema for the config file format by
// reflecting over ConfigV1. Adding, removing or retagging a field changes the
// generated schema without any other edits; see schemaOverrides above for the
// handful of constraints reflection can't express on its own.
func GenerateConfigSchema() (map[string]any, error) {
	cfg, err := structSchema(reflect.TypeOf(ConfigV1{}), "")
	if err != nil {
		return nil, err
	}
	cfg["properties"].(map[string]any)["version"] = map[string]any{
		"type":        "integer",
		"enum":        []any{int(LatestConfigVersion)},
		"description": "Config schema version. Selects which fields the rest of the document is validated against.",
	}
	required := cfg["required"].([]any)
	cfg["required"] = append([]any{"version"}, required...)

	schema := map[string]any{
		"$schema":     "https://json-schema.org/draft/2020-12/schema",
		"$id":         "https://github.com/Cyb3r-Jak3/email-backup-tool/config.schema.json",
		"title":       "email-backup-tool config file",
		"description": "Config file for imap-backup-tool. See example.yaml for a fully annotated example.",
	}
	maps.Copy(schema, cfg)
	return schema, nil
}

// structSchema builds the "object" schema for a struct type, keyed by yaml
// tag name. path is the dotted yaml path to this struct, used to look up
// schemaOverrides and to report field errors.
func structSchema(t reflect.Type, path string) (map[string]any, error) {
	if t.Kind() != reflect.Struct {
		return nil, fmt.Errorf("%s: expected a struct, got %s", path, t.Kind())
	}
	properties := map[string]any{}
	required := []any{}

	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		yamlTag := field.Tag.Get("yaml")
		if yamlTag == "" || yamlTag == "-" {
			continue
		}
		parts := strings.Split(yamlTag, ",")
		name := parts[0]
		omitempty := false
		for _, opt := range parts[1:] {
			if opt == "omitempty" {
				omitempty = true
			}
		}
		fieldPath := name
		if path != "" {
			fieldPath = path + "." + name
		}

		fieldSchema, err := fieldSchema(field.Type, fieldPath)
		if err != nil {
			return nil, err
		}
		applyTags(fieldSchema, field.Tag)
		properties[name] = fieldSchema

		isRequired := !omitempty
		if v := field.Tag.Get("required"); v != "" {
			isRequired, err = strconv.ParseBool(v)
			if err != nil {
				return nil, fmt.Errorf("%s: invalid required tag %q: %w", fieldPath, v, err)
			}
		}
		if isRequired {
			required = append(required, name)
		}
	}

	schema := map[string]any{
		"type":                 "object",
		"properties":           properties,
		"required":             required,
		"additionalProperties": false,
	}
	for k, v := range schemaOverrides[path] {
		schema[k] = v
	}
	return schema, nil
}

// fieldSchema builds the schema for one field's value type, recursing into
// structs (and structs behind a pointer) so nested objects like login.imap
// get their own properties/required.
func fieldSchema(t reflect.Type, path string) (map[string]any, error) {
	if t.Kind() == reflect.Pointer {
		return fieldSchema(t.Elem(), path)
	}
	switch t.Kind() {
	case reflect.String:
		return map[string]any{"type": "string"}, nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return map[string]any{"type": "integer"}, nil
	case reflect.Bool:
		return map[string]any{"type": "boolean"}, nil
	case reflect.Struct:
		return structSchema(t, path)
	default:
		return nil, fmt.Errorf("%s: unsupported field kind %s in config schema generator", path, t.Kind())
	}
}

// applyTags copies the schema-only struct tags (desc, enum, pattern, min,
// max, default) from a field onto its generated schema fragment.
func applyTags(schema map[string]any, tag reflect.StructTag) {
	if v := tag.Get("desc"); v != "" {
		schema["description"] = v
	}
	if v := tag.Get("enum"); v != "" {
		values := strings.Split(v, ",")
		enum := make([]any, len(values))
		for i, e := range values {
			enum[i] = e
		}
		schema["enum"] = enum
	}
	if v := tag.Get("pattern"); v != "" {
		schema["pattern"] = v
	}
	if v := tag.Get("min"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			schema["minimum"] = n
		}
	}
	if v := tag.Get("max"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			schema["maximum"] = n
		}
	}
	if v := tag.Get("default"); v != "" {
		if schema["type"] == "integer" {
			if n, err := strconv.Atoi(v); err == nil {
				schema["default"] = n
				return
			}
		}
		schema["default"] = v
	}
}
