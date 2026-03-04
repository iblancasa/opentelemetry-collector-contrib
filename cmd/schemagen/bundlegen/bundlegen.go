// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/open-telemetry/opentelemetry-collector-contrib/cmd/schemagen/internal"
	"gopkg.in/yaml.v3"
)

var schemaFileNames = []string{
	"config.schema.json",
	"config.schema.yaml",
	"config.schema.yml",
}

func GenerateBundle(configPath, outPath, fileType string, moduleRoots map[string]string, includeAll, allowMissing, inline bool) error {
	cfgAbs, err := filepath.Abs(configPath)
	if err != nil && configPath != "" {
		return err
	}
	outAbs, err := filepath.Abs(outPath)
	if err != nil {
		return err
	}
	if fileType == "" {
		fileType = strings.TrimPrefix(strings.ToLower(filepath.Ext(outAbs)), ".")
	}
	if fileType == "" {
		return errors.New("output format could not be inferred from extension, use -t")
	}

	var rootMap map[string]any
	if !includeAll {
		configDoc, err := loadDocument(cfgAbs)
		if err != nil {
			return fmt.Errorf("load config: %w", err)
		}
		var ok bool
		rootMap, ok = configDoc.(map[string]any)
		if !ok {
			return errors.New("config must be a map at the root")
		}
	}

	refs, err := collectComponentRefs(rootMap, moduleRoots, includeAll, allowMissing)
	if err != nil {
		return err
	}

	schema := buildSchemaBundle(refs, allowMissing)
	if inline {
		inlined, err := InlineSchema(schema, moduleRoots, filepath.Dir(outAbs), allowMissing)
		if err != nil {
			return err
		}
		schema = inlined
	}
	applyDurationPattern(schema)
	applyMapListAnyOf(schema)
	applyReceiverCreatorSchemaFix(schema)
	relaxEmbeddedAdditionalProperties(schema)

	raw, err := marshalSchema(schema, fileType)
	if err != nil {
		return err
	}
	if err := os.WriteFile(outAbs, raw, 0o644); err != nil {
		return err
	}
	return nil
}

type schemaEntry struct {
	Ref    string
	Inline map[string]any
}

type componentRefs struct {
	Receivers  map[string]schemaEntry
	Processors map[string]schemaEntry
	Exporters  map[string]schemaEntry
	Extensions map[string]schemaEntry
	Connectors map[string]schemaEntry
}

type schemaIndex struct {
	byKind map[string]map[string]schemaEntry
}

func collectComponentRefs(root map[string]any, moduleRoots map[string]string, includeAll, allowMissing bool) (componentRefs, error) {
	refs := componentRefs{
		Receivers:  make(map[string]schemaEntry),
		Processors: make(map[string]schemaEntry),
		Exporters:  make(map[string]schemaEntry),
		Extensions: make(map[string]schemaEntry),
		Connectors: make(map[string]schemaEntry),
	}

	index, err := buildSchemaIndex(moduleRoots)
	if err != nil {
		return refs, err
	}

	if includeAll {
		if err := addAllKindRefs("receiver", index, refs.Receivers); err != nil {
			return refs, err
		}
		if err := addAllKindRefs("processor", index, refs.Processors); err != nil {
			return refs, err
		}
		if err := addAllKindRefs("exporter", index, refs.Exporters); err != nil {
			return refs, err
		}
		if err := addAllKindRefs("extension", index, refs.Extensions); err != nil {
			return refs, err
		}
		if err := addAllKindRefs("connector", index, refs.Connectors); err != nil {
			return refs, err
		}
		return refs, nil
	}

	if err := addKindRefs("receivers", "receiver", root, index, refs.Receivers, allowMissing); err != nil {
		return refs, err
	}
	if err := addKindRefs("processors", "processor", root, index, refs.Processors, allowMissing); err != nil {
		return refs, err
	}
	if err := addKindRefs("exporters", "exporter", root, index, refs.Exporters, allowMissing); err != nil {
		return refs, err
	}
	if err := addKindRefs("extensions", "extension", root, index, refs.Extensions, allowMissing); err != nil {
		return refs, err
	}
	if err := addKindRefs("connectors", "connector", root, index, refs.Connectors, allowMissing); err != nil {
		return refs, err
	}
	return refs, nil
}

func addKindRefs(section, kind string, root map[string]any, index schemaIndex, out map[string]schemaEntry, allowMissing bool) error {
	raw, ok := root[section]
	if !ok {
		return nil
	}
	components, ok := raw.(map[string]any)
	if !ok {
		return fmt.Errorf("%s must be a map", section)
	}
	for id := range components {
		typ := componentType(id)
		if entry, found := index.lookup(kind, typ); found {
			out[id] = entry
			continue
		}
		if allowMissing {
			out[id] = schemaEntry{Inline: map[string]any{"type": "object"}}
			continue
		}
		return fmt.Errorf("%s %q: schema not found", kind, id)
	}
	return nil
}

func componentType(id string) string {
	if idx := strings.Index(id, "/"); idx >= 0 {
		return id[:idx]
	}
	return id
}

func (s schemaIndex) lookup(kind, typ string) (schemaEntry, bool) {
	if s.byKind == nil {
		return schemaEntry{}, false
	}
	types, ok := s.byKind[kind]
	if !ok {
		return schemaEntry{}, false
	}
	entry, ok := types[typ]
	return entry, ok
}

func buildSchemaBundle(refs componentRefs, allowMissing bool) map[string]any {
	receiverIDs := expandComponentTypeAliases("receiver", sortedKeys(refs.Receivers))
	processorIDs := expandComponentTypeAliases("processor", sortedKeys(refs.Processors))
	exporterIDs := sortedKeys(refs.Exporters)
	extensionIDs := sortedKeys(refs.Extensions)
	connectorIDs := sortedKeys(refs.Connectors)

	receiverTypes := expandComponentTypeAliases("receiver", collectTypesFromRefs(refs.Receivers))
	processorTypes := expandComponentTypeAliases("processor", collectTypesFromRefs(refs.Processors))
	exporterTypes := collectTypesFromRefs(refs.Exporters)
	extensionTypes := expandComponentTypeAliases("extension", collectTypesFromRefs(refs.Extensions))
	connectorTypes := collectTypesFromRefs(refs.Connectors)
	receiverOrConnectorIDs := expandTypeAliases(append([]string{}, append(receiverIDs, connectorIDs...)...))
	exporterOrConnectorIDs := expandTypeAliases(append([]string{}, append(exporterIDs, connectorIDs...)...))

	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"receivers":  buildSection(refs.Receivers, receiverTypes, allowMissing),
			"processors": buildSection(refs.Processors, processorTypes, allowMissing),
			"exporters":  buildSection(refs.Exporters, exporterTypes, allowMissing),
			"extensions": buildSection(refs.Extensions, extensionTypes, allowMissing),
			"connectors": buildSection(refs.Connectors, connectorTypes, allowMissing),
			"service": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"extensions": map[string]any{
						"type":  "array",
						"items": patternSchema(extensionIDs, allowMissing),
					},
					"pipelines": map[string]any{
						"type": "object",
						"additionalProperties": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"receivers":  arrayPatternSchema(receiverOrConnectorIDs, allowMissing),
								"processors": arrayPatternSchema(processorIDs, allowMissing),
								"exporters":  arrayPatternSchema(exporterOrConnectorIDs, allowMissing),
							},
						},
					},
				},
			},
		},
	}
}

func buildSection(refs map[string]schemaEntry, types []string, allowMissing bool) map[string]any {
	props := make(map[string]any, len(refs))
	for id, entry := range refs {
		if entry.Ref != "" {
			props[id] = allowNull(map[string]any{"$ref": entry.Ref})
			continue
		}
		if entry.Inline != nil {
			props[id] = allowNull(entry.Inline)
			continue
		}
		props[id] = allowNull(map[string]any{"type": "object"})
	}
	return map[string]any{
		"type":                 "object",
		"properties":           props,
		"patternProperties":    patternProperties(types, allowMissing),
		"additionalProperties": false,
	}
}

func marshalSchema(schema map[string]any, fileType string) ([]byte, error) {
	switch fileType {
	case "json":
		return json.MarshalIndent(schema, "", "  ")
	case "yaml", "yml":
		return yaml.Marshal(schema)
	default:
		return nil, fmt.Errorf("unsupported output format %q", fileType)
	}
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
	for _, name := range schemaFileNames {
		candidate := filepath.Join(path, "config", name)
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

func addAllKindRefs(kind string, index schemaIndex, out map[string]schemaEntry) error {
	if index.byKind == nil {
		return nil
	}
	entries, ok := index.byKind[kind]
	if !ok {
		return nil
	}
	for typ, entry := range entries {
		out[typ] = entry
	}
	return nil
}

func buildSchemaIndex(moduleRoots map[string]string) (schemaIndex, error) {
	index := schemaIndex{byKind: map[string]map[string]schemaEntry{}}
	type rootEntry struct {
		prefix string
		root   string
	}
	var roots []rootEntry
	for prefix, root := range moduleRoots {
		roots = append(roots, rootEntry{prefix: prefix, root: root})
	}
	sort.Slice(roots, func(i, j int) bool {
		return roots[i].prefix < roots[j].prefix
	})

	kinds := []string{"receiver", "processor", "exporter", "extension", "connector"}
	for _, entry := range roots {
		settings := readSettingsAt(entry.root)
		for _, kind := range kinds {
			base := filepath.Join(entry.root, kind)
			_ = filepath.WalkDir(base, func(path string, d os.DirEntry, err error) error {
				if err != nil || d == nil || !d.IsDir() {
					return nil
				}
				md, ok := internal.ReadMetadata(path)
				if !ok || md.Status.Class != kind {
					return nil
				}
				schemaDir, ok := findComponentSchemaDir(path, md, settings)
				if !ok {
					return filepath.SkipDir
				}
				rel, err := filepath.Rel(entry.root, schemaDir)
				if err != nil {
					return filepath.SkipDir
				}
				ref := fmt.Sprintf("%s/%s", entry.prefix, filepath.ToSlash(rel))
				if index.byKind[kind] == nil {
					index.byKind[kind] = map[string]schemaEntry{}
				}
				if _, exists := index.byKind[kind][md.Type]; !exists {
					index.byKind[kind][md.Type] = schemaEntry{Ref: ref}
				}
				return filepath.SkipDir
			})
		}
	}
	return index, nil
}

func readSettingsAt(root string) *internal.Settings {
	orig, _ := os.Getwd()
	_ = os.Chdir(root)
	settings, _ := internal.ReadSettingsFile()
	_ = os.Chdir(orig)
	return settings
}

func findComponentSchemaDir(dir string, md *internal.Metadata, settings *internal.Settings) (string, bool) {
	var candidates []string
	configDir := ""
	if settings != nil {
		comp := md.Status.Class + "/" + md.Type
		if override, found := settings.ComponentOverrides[comp]; found {
			configDir = override.ConfigDir
			if configDir == "" && override.ConfigName != "" {
				parts := strings.Split(override.ConfigName, ".")
				if len(parts) == 2 {
					configDir = parts[0]
				}
			}
		}
	}
	if configDir != "" {
		if filepath.IsAbs(configDir) {
			candidates = append(candidates, configDir)
		} else {
			candidates = append(candidates, filepath.Join(dir, configDir))
		}
	}
	candidates = append(candidates, dir, filepath.Join(dir, "config"))
	for _, candidate := range candidates {
		if _, ok := findSchemaFile(candidate); ok {
			return candidate, true
		}
	}
	return "", false
}

func sortedKeys(m map[string]schemaEntry) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func enumSchema(values []string) map[string]any {
	if len(values) == 0 {
		return map[string]any{"type": "string"}
	}
	enum := make([]any, 0, len(values))
	for _, v := range values {
		enum = append(enum, v)
	}
	return map[string]any{"enum": enum}
}

func arrayEnumSchema(values []string) map[string]any {
	return map[string]any{
		"type":  "array",
		"items": enumSchema(values),
	}
}

func patternProperties(values []string, allowMissing bool) map[string]any {
	pat := patternFromValues(values, allowMissing)
	if pat == "" {
		return nil
	}
	return map[string]any{
		pat: allowNull(map[string]any{"type": "object"}),
	}
}

func patternSchema(values []string, allowMissing bool) map[string]any {
	pat := patternFromValues(values, allowMissing)
	if pat == "" {
		return map[string]any{"type": "string"}
	}
	return map[string]any{
		"type":    "string",
		"pattern": pat,
	}
}

func arrayPatternSchema(values []string, allowMissing bool) map[string]any {
	return map[string]any{
		"type":  "array",
		"items": patternSchema(values, allowMissing),
	}
}

func patternFromValues(values []string, allowMissing bool) string {
	if allowMissing {
		return "^([A-Za-z0-9_\\-]+)(/.*)?$"
	}
	if len(values) == 0 {
		return ""
	}
	unique := make(map[string]struct{}, len(values))
	for _, v := range values {
		unique[v] = struct{}{}
	}
	list := make([]string, 0, len(unique))
	for v := range unique {
		list = append(list, regexp.QuoteMeta(v))
	}
	sort.Strings(list)
	return fmt.Sprintf("^(%s)(/.*)?$", strings.Join(list, "|"))
}

func collectTypesFromRefs(refs map[string]schemaEntry) []string {
	types := make(map[string]struct{}, len(refs))
	for id := range refs {
		base := componentType(id)
		types[base] = struct{}{}
		for _, alias := range aliasTypes(base) {
			types[alias] = struct{}{}
		}
	}
	// Include aliases that exist even if only "new" names are present.
	for _, a := range knownAliases() {
		types[a] = struct{}{}
	}
	out := make([]string, 0, len(types))
	for t := range types {
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}

func expandTypeAliases(values []string) []string {
	unique := make(map[string]struct{}, len(values))
	for _, v := range values {
		unique[v] = struct{}{}
		for _, alias := range aliasTypes(v) {
			unique[alias] = struct{}{}
		}
	}
	for _, alias := range knownAliases() {
		unique[alias] = struct{}{}
	}
	out := make([]string, 0, len(unique))
	for v := range unique {
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}

func expandComponentTypeAliases(kind string, values []string) []string {
	unique := make(map[string]struct{}, len(values))
	for _, v := range values {
		unique[v] = struct{}{}
		for _, alias := range componentAliases(kind, v) {
			unique[alias] = struct{}{}
		}
	}
	for _, extra := range extraComponentTypes(kind) {
		unique[extra] = struct{}{}
	}
	out := make([]string, 0, len(unique))
	for v := range unique {
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}

func componentAliases(kind, base string) []string {
	switch kind {
	case "processor":
		if base == "k8s_attributes" {
			return []string{"k8sattributes"}
		}
	}
	return nil
}

func extraComponentTypes(kind string) []string {
	return nil
}

func aliasTypes(base string) []string {
	switch base {
	case "otlp_http":
		return []string{"otlphttp"}
	case "otlp_grpc":
		return []string{"otlpgrpc", "otlp"}
	}
	if strings.Contains(base, "_") {
		return nil
	}
	var aliases []string
	if strings.HasSuffix(base, "http") && len(base) > len("http") {
		aliases = append(aliases, strings.TrimSuffix(base, "http")+"_http")
	}
	if strings.HasSuffix(base, "grpc") && len(base) > len("grpc") {
		aliases = append(aliases, strings.TrimSuffix(base, "grpc")+"_grpc")
	}
	return aliases
}

func knownAliases() []string {
	return []string{"otlphttp", "otlpgrpc", "otlp"}
}

func allowNull(schema map[string]any) map[string]any {
	return map[string]any{
		"anyOf": []any{
			schema,
			map[string]any{"type": "null"},
		},
	}
}

// Add a Go-duration-compatible regex for validators that don't implement format=duration.
func applyDurationPattern(node any) {
	switch v := node.(type) {
	case map[string]any:
		if fmtVal, ok := v["format"].(string); ok && fmtVal == "duration" {
			if _, has := v["anyOf"]; !has {
				// Go time.Duration format: sequence of number+unit, with optional sign and decimals.
				pattern := `^[-+]?((\d+(\.\d+)?|\.\d+)(ns|us|µs|ms|s|m|h))+$`
				strSchema := map[string]any{
					"type":    "string",
					"pattern": pattern,
				}
				intSchema := map[string]any{
					"type": "integer",
				}
				v["anyOf"] = []any{strSchema, intSchema}
				delete(v, "type")
				delete(v, "pattern")
			}
			delete(v, "format")
		}
		for _, val := range v {
			applyDurationPattern(val)
		}
	case []any:
		for _, val := range v {
			applyDurationPattern(val)
		}
	}
}

// Allow configopaque.MapList to be provided as either a list of {name,value} pairs or a map.
func applyMapListAnyOf(root any) {
	const mapListDesc = "MapList is a replacement for map[string]configopaque.String"

	var visit func(node any)
	visit = func(node any) {
		switch v := node.(type) {
		case map[string]any:
			if desc, ok := v["description"].(string); ok && strings.Contains(desc, mapListDesc) {
				if _, has := v["anyOf"]; !has {
					arraySchema := map[string]any{
						"type":  "array",
						"items": v["items"],
					}
					objectSchema := map[string]any{
						"type":                 "object",
						"additionalProperties": map[string]any{"type": "string"},
					}
					delete(v, "type")
					delete(v, "items")
					v["anyOf"] = []any{arraySchema, objectSchema}
				}
			}
			for _, val := range v {
				visit(val)
			}
		case []any:
			for _, val := range v {
				visit(val)
			}
		}
	}

	visit(root)
}

// Ensure receiver_creator schema allows dynamic subreceivers.
func applyReceiverCreatorSchemaFix(root any) {
	const rcDesc = "Config defines configuration for receiver_creator."
	const rcProp = "receivers"

	var visit func(node any)
	visit = func(node any) {
		switch v := node.(type) {
		case map[string]any:
			if desc, ok := v["description"].(string); ok && desc == rcDesc {
				props, _ := v["properties"].(map[string]any)
				if props == nil {
					props = map[string]any{}
					v["properties"] = props
				}
				if _, exists := props[rcProp]; !exists {
					props[rcProp] = map[string]any{
						"type": "object",
						"additionalProperties": map[string]any{
							"type":                 "object",
							"additionalProperties": true,
						},
					}
				}
			}
			for _, val := range v {
				visit(val)
			}
		case []any:
			for _, val := range v {
				visit(val)
			}
		}
	}

	visit(root)
}

// When schemas are composed with allOf, embedded schemas should not lock down
// additionalProperties, otherwise shared configs (like http/grpc) reject extra fields.
func relaxEmbeddedAdditionalProperties(root map[string]any) {
	defs, _ := root["$defs"].(map[string]any)
	if defs == nil {
		return
	}

	type localRef struct {
		parent string
		local  string
	}
	refDefs := map[string]struct{}{}
	refLocals := []localRef{}

	walkAllOfRefs(root, func(ref string) {
		if strings.HasPrefix(ref, "#/$defs/") {
			parts := strings.Split(ref[len("#/$defs/"):], "/")
			if len(parts) == 1 {
				refDefs[parts[0]] = struct{}{}
			}
			if len(parts) >= 3 && parts[1] == "$defs" {
				refLocals = append(refLocals, localRef{parent: parts[0], local: parts[2]})
			}
		}
	})

	for def := range refDefs {
		if defSchema, ok := defs[def].(map[string]any); ok {
			delete(defSchema, "additionalProperties")
		}
	}

	for _, lr := range refLocals {
		if defSchema, ok := defs[lr.parent].(map[string]any); ok {
			if localDefs, ok := defSchema["$defs"].(map[string]any); ok {
				if localSchema, ok := localDefs[lr.local].(map[string]any); ok {
					delete(localSchema, "additionalProperties")
				}
			}
		}
	}
}

func walkAllOfRefs(node any, fn func(ref string)) {
	switch v := node.(type) {
	case map[string]any:
		if allOf, ok := v["allOf"].([]any); ok {
			for _, item := range allOf {
				if m, ok := item.(map[string]any); ok {
					if ref, ok := m["$ref"].(string); ok {
						fn(ref)
					}
				}
			}
		}
		for _, val := range v {
			walkAllOfRefs(val, fn)
		}
	case []any:
		for _, val := range v {
			walkAllOfRefs(val, fn)
		}
	}
}
