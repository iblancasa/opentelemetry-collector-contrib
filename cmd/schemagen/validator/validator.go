// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"gopkg.in/yaml.v3"
)

var schemaFileNames = []string{
	"config.schema.json",
	"config.schema.yaml",
	"config.schema.yml",
}

func Validate(schemaPath, configPath string, moduleRoots map[string]string) error {
	schemaAbs, err := filepath.Abs(schemaPath)
	if err != nil {
		return err
	}
	configAbs, err := filepath.Abs(configPath)
	if err != nil {
		return err
	}

	schemaDoc, err := loadDocument(schemaAbs)
	if err != nil {
		return fmt.Errorf("load schema: %w", err)
	}
	configDoc, err := loadDocument(configAbs)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	compiler := jsonschema.NewCompiler()
	compiler.UseLoader(&schemaLoader{
		moduleRoots: moduleRoots,
	})
	compiler.RegisterFormat(&jsonschema.Format{
		Name:     "duration",
		Validate: validateDuration,
	})
	compiler.AssertFormat()
	if err := compiler.AddResource(schemaAbs, schemaDoc); err != nil {
		return fmt.Errorf("add schema resource: %w", err)
	}

	schema, err := compiler.Compile(schemaAbs)
	if err != nil {
		return fmt.Errorf("compile schema: %w", err)
	}
	if err := schema.Validate(configDoc); err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}
	return nil
}

type schemaLoader struct {
	moduleRoots map[string]string
}

func (l *schemaLoader) Load(rawURL string) (any, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	if u.Scheme != "file" {
		return nil, fmt.Errorf("unsupported scheme %q in %q", u.Scheme, rawURL)
	}
	path, err := urlPathToFile(u)
	if err != nil {
		return nil, err
	}

	if doc, ok, err := l.tryLoadExisting(path); err != nil {
		return nil, err
	} else if ok {
		return doc, nil
	}

	if doc, ok, err := l.tryModuleRef(path, rawURL); err != nil {
		return nil, err
	} else if ok {
		return doc, nil
	}

	if doc, ok, err := l.tryDotRef(path, rawURL); err != nil {
		return nil, err
	} else if ok {
		return doc, nil
	}

	return nil, os.ErrNotExist
}

func (l *schemaLoader) tryLoadExisting(path string) (any, bool, error) {
	if fileExists(path) {
		doc, err := loadDocument(path)
		return doc, err == nil, err
	}
	if ext := filepath.Ext(path); ext != "" {
		return nil, false, nil
	}
	for _, name := range schemaFileNames {
		p := filepath.Join(path, name)
		if fileExists(p) {
			doc, err := loadDocument(p)
			return doc, err == nil, err
		}
	}
	return nil, false, nil
}

func (l *schemaLoader) tryModuleRef(path, rawURL string) (any, bool, error) {
	if len(l.moduleRoots) == 0 {
		return nil, false, nil
	}
	for prefix, root := range l.moduleRoots {
		idx := strings.Index(path, prefix)
		if idx == -1 {
			continue
		}
		ref := filepath.ToSlash(path[idx:])
		doc, ok, err := l.syntheticRefDoc(prefix, root, ref, rawURL)
		return doc, ok, err
	}
	return nil, false, nil
}

func (l *schemaLoader) tryDotRef(path, rawURL string) (any, bool, error) {
	base, def, ok := splitRefPath(path)
	if !ok {
		base = path
		def = ""
	}
	schemaPath, ok := findSchemaFile(base)
	if !ok {
		if def == "" {
			dir := filepath.Dir(base)
			if schemaPath, ok := findSchemaFile(dir); ok {
				def = filepath.Base(base)
				return syntheticRef(rawURL, schemaPath, def), true, nil
			}
		}
		return nil, false, nil
	}
	return syntheticRef(rawURL, schemaPath, def), true, nil
}

func (l *schemaLoader) syntheticRefDoc(prefix, root, ref, rawURL string) (any, bool, error) {
	base, def, ok := splitRefPath(ref)
	if !ok {
		base = ref
		def = ""
	}
	sub := strings.TrimPrefix(base, prefix)
	sub = strings.TrimPrefix(sub, "/")
	schemaPath, ok := findSchemaFile(filepath.Join(root, filepath.FromSlash(sub)))
	if !ok {
		return nil, false, nil
	}
	return syntheticRef(rawURL, schemaPath, def), true, nil
}

func syntheticRef(rawURL, schemaPath, def string) map[string]any {
	schemaURL := fileURL(schemaPath)
	ref := schemaURL
	if def != "" {
		ref = fmt.Sprintf("%s#/$defs/%s", schemaURL, def)
	}
	return map[string]any{
		"$id":  rawURL,
		"$ref": ref,
	}
}

func splitRefPath(path string) (base string, def string, ok bool) {
	slash := strings.LastIndex(path, "/")
	last := path
	if slash >= 0 {
		last = path[slash+1:]
	}
	dot := strings.LastIndex(last, ".")
	if dot <= 0 || dot == len(last)-1 {
		return "", "", false
	}
	base = path[:slash+1] + last[:dot]
	def = last[dot+1:]
	return base, def, true
}

func findSchemaFile(path string) (string, bool) {
	if fileExists(path) {
		return path, true
	}
	if ext := filepath.Ext(path); ext != "" {
		return "", false
	}
	for _, name := range schemaFileNames {
		candidate := filepath.Join(path, name)
		if fileExists(candidate) {
			return candidate, true
		}
	}
	return "", false
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func fileURL(path string) string {
	u := url.URL{
		Scheme: "file",
		Path:   filepath.ToSlash(path),
	}
	return u.String()
}

func urlPathToFile(u *url.URL) (string, error) {
	if u.Scheme != "file" {
		return "", fmt.Errorf("unsupported scheme %q", u.Scheme)
	}
	path := u.Path
	if runtime.GOOS == "windows" {
		path = strings.TrimPrefix(path, "/")
	}
	return filepath.FromSlash(path), nil
}

func loadDocument(path string) (any, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	ext := strings.ToLower(filepath.Ext(path))
	if ext == ".json" {
		var v any
		if err := json.Unmarshal(raw, &v); err != nil {
			return nil, err
		}
		return v, nil
	}
	var v any
	if err := yaml.Unmarshal(raw, &v); err != nil {
		return nil, err
	}
	return normalizeYAML(v)
}

func normalizeYAML(v any) (any, error) {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, v := range t {
			n, err := normalizeYAML(v)
			if err != nil {
				return nil, err
			}
			out[k] = n
		}
		return out, nil
	case map[any]any:
		out := make(map[string]any, len(t))
		for k, v := range t {
			ks, ok := k.(string)
			if !ok {
				return nil, errors.New("non-string map key in YAML")
			}
			n, err := normalizeYAML(v)
			if err != nil {
				return nil, err
			}
			out[ks] = n
		}
		return out, nil
	case []any:
		out := make([]any, len(t))
		for i, v := range t {
			n, err := normalizeYAML(v)
			if err != nil {
				return nil, err
			}
			out[i] = n
		}
		return out, nil
	default:
		return v, nil
	}
}

func validateDuration(v any) error {
	s, ok := v.(string)
	if !ok {
		return fmt.Errorf("duration must be a string, got %T", v)
	}
	if _, err := time.ParseDuration(s); err != nil {
		return fmt.Errorf("invalid duration format: %w", err)
	}
	return nil
}
