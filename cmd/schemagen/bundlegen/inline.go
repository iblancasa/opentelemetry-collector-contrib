// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

type inlineState struct {
	defs         map[string]any
	defByRef     map[string]string
	moduleRoots  map[string]string
	baseDir      string
	allowMissing bool
}

func InlineSchema(schema map[string]any, moduleRoots map[string]string, baseDir string, allowMissing bool) (map[string]any, error) {
	state := &inlineState{
		defs:         map[string]any{},
		defByRef:     map[string]string{},
		moduleRoots:  moduleRoots,
		baseDir:      baseDir,
		allowMissing: allowMissing,
	}

	out, err := state.inlineNode(schema, baseDir, "")
	if err != nil {
		return nil, err
	}

	root, ok := out.(map[string]any)
	if !ok {
		return nil, errors.New("inlined schema root must be an object")
	}

	if len(state.defs) > 0 {
		if existing, ok := root["$defs"].(map[string]any); ok {
			for k, v := range existing {
				if _, exists := state.defs[k]; !exists {
					state.defs[k] = v
				}
			}
		}
		root["$defs"] = state.defs
	}
	fillMissingDefs(root)

	return root, nil
}

func (s *inlineState) inlineNode(node any, baseDir, defName string) (any, error) {
	switch v := node.(type) {
	case map[string]any:
		if refRaw, ok := v["$ref"].(string); ok {
			if strings.HasPrefix(refRaw, "#") {
				if defName == "" {
					return v, nil
				}
				v["$ref"] = rewriteLocalRef(refRaw, defName)
				return v, nil
			}
			if isShortRef(refRaw) {
				local := "#/$defs/" + refRaw
				if defName != "" {
					local = rewriteLocalRef(local, defName)
				}
				return map[string]any{"$ref": local}, nil
			}
			defKey, err := s.addRef(refRaw, baseDir)
			if err != nil {
				return nil, err
			}
			return map[string]any{"$ref": "#/$defs/" + defKey}, nil
		}

		out := make(map[string]any, len(v))
		for k, val := range v {
			if k == "$defs" {
				defsMap, ok := val.(map[string]any)
				if !ok {
					out[k] = val
					continue
				}
				outDefs := make(map[string]any, len(defsMap))
				for def, defVal := range defsMap {
					defDoc, err := s.inlineNode(defVal, baseDir, defName)
					if err != nil {
						return nil, err
					}
					outDefs[def] = defDoc
				}
				out[k] = outDefs
				continue
			}
			child, err := s.inlineNode(val, baseDir, defName)
			if err != nil {
				return nil, err
			}
			out[k] = child
		}
		return out, nil
	case []any:
		out := make([]any, len(v))
		for i, val := range v {
			child, err := s.inlineNode(val, baseDir, defName)
			if err != nil {
				return nil, err
			}
			out[i] = child
		}
		return out, nil
	default:
		return node, nil
	}
}

func (s *inlineState) addRef(refRaw, baseDir string) (string, error) {
	if def, ok := s.defByRef[refRaw]; ok {
		return def, nil
	}

	doc, refBaseDir, err := resolveRefDoc(refRaw, baseDir, s.moduleRoots)
	if err != nil {
		if s.allowMissing {
			defKey := defNameForRef(refRaw, s.defs)
			s.defByRef[refRaw] = defKey
			s.defs[defKey] = map[string]any{
				"type":         "object",
				"x-missingRef": refRaw,
			}
			return defKey, nil
		}
		return "", err
	}

	defKey := defNameForRef(refRaw, s.defs)
	s.defByRef[refRaw] = defKey

	inlined, err := s.inlineNode(doc, refBaseDir, defKey)
	if err != nil {
		return "", err
	}
	s.defs[defKey] = inlined

	return defKey, nil
}

func resolveRefDoc(refRaw, baseDir string, moduleRoots map[string]string) (any, string, error) {
	if strings.HasPrefix(refRaw, "#") {
		return nil, baseDir, fmt.Errorf("unexpected local ref %q in external resolver", refRaw)
	}

	if strings.HasPrefix(refRaw, "./") || strings.HasPrefix(refRaw, "../") {
		return resolveFileRef(filepath.Join(baseDir, refRaw))
	}

	for prefix, root := range moduleRoots {
		if strings.HasPrefix(refRaw, prefix) {
			sub := strings.TrimPrefix(refRaw, prefix)
			sub = strings.TrimPrefix(sub, "/")
			return resolveFileRef(filepath.Join(root, filepath.FromSlash(sub)))
		}
	}

	return resolveFileRef(filepath.Join(baseDir, refRaw))
}

func resolveFileRef(path string) (any, string, error) {
	base, def, ok := splitRefPath(filepath.ToSlash(path))
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
				return loadDef(schemaPath, def)
			}
		}
		return nil, base, fmt.Errorf("schema not found for ref %q", path)
	}
	if def == "" {
		doc, err := loadDocument(schemaPath)
		return doc, filepath.Dir(schemaPath), err
	}
	return loadDef(schemaPath, def)
}

func loadDef(schemaPath, def string) (any, string, error) {
	doc, err := loadDocument(schemaPath)
	if err != nil {
		return nil, "", err
	}
	root, ok := doc.(map[string]any)
	if !ok {
		return nil, "", errors.New("schema root is not an object")
	}
	defs, ok := root["$defs"].(map[string]any)
	if !ok {
		if def == "config" {
			return root, filepath.Dir(schemaPath), nil
		}
		return nil, "", fmt.Errorf("missing $defs in schema %q", schemaPath)
	}
	defDoc, ok := defs[def]
	if !ok {
		return nil, "", fmt.Errorf("missing $defs/%s in schema %q", def, schemaPath)
	}
	if defMap, ok := defDoc.(map[string]any); ok {
		if _, has := defMap["$defs"]; !has {
			defMap["$defs"] = copyDefsWithout(defs, def)
		}
		defDoc = defMap
	}
	return defDoc, filepath.Dir(schemaPath), nil
}

func rewriteLocalRef(refRaw, defName string) string {
	if refRaw == "#" {
		return "#/$defs/" + defName
	}
	if strings.HasPrefix(refRaw, "#/") {
		return "#/$defs/" + defName + refRaw[1:]
	}
	return refRaw
}

func isShortRef(refRaw string) bool {
	if strings.HasPrefix(refRaw, "#") {
		return false
	}
	if strings.Contains(refRaw, "/") || strings.Contains(refRaw, ".") {
		return false
	}
	return true
}

func fillMissingDefs(root map[string]any) {
	defs, _ := root["$defs"].(map[string]any)
	walkRefs(root, func(ref string) {
		if strings.HasPrefix(ref, "#/$defs/") {
			parts := strings.Split(ref[len("#/$defs/"):], "/")
			if len(parts) == 1 {
				key := parts[0]
				if _, ok := defs[key]; !ok {
					defs[key] = map[string]any{"type": "object", "x-missingRef": ref}
				}
				return
			}
			if len(parts) >= 3 && parts[1] == "$defs" {
				defName := parts[0]
				localDef := parts[2]
				if defSchema, ok := defs[defName].(map[string]any); ok {
					localDefs, _ := defSchema["$defs"].(map[string]any)
					if localDefs == nil {
						localDefs = map[string]any{}
						defSchema["$defs"] = localDefs
					}
					if _, ok := localDefs[localDef]; !ok {
						localDefs[localDef] = map[string]any{"type": "object", "x-missingRef": ref}
					}
				}
			}
		}
	})
}

func walkRefs(node any, fn func(ref string)) {
	switch v := node.(type) {
	case map[string]any:
		if ref, ok := v["$ref"].(string); ok {
			fn(ref)
		}
		for _, val := range v {
			walkRefs(val, fn)
		}
	case []any:
		for _, val := range v {
			walkRefs(val, fn)
		}
	}
}

func copyDefsWithout(defs map[string]any, exclude string) map[string]any {
	out := make(map[string]any, len(defs))
	for k, v := range defs {
		if k == exclude {
			continue
		}
		out[k] = v
	}
	return out
}

func defNameForRef(refRaw string, existing map[string]any) string {
	base := sanitizeDefName(refRaw)
	if _, ok := existing[base]; !ok {
		return base
	}
	h := sha1.Sum([]byte(refRaw))
	suffix := hex.EncodeToString(h[:6])
	return base + "_" + suffix
}

func sanitizeDefName(ref string) string {
	trimmed := strings.TrimPrefix(ref, "https://")
	trimmed = strings.TrimPrefix(trimmed, "http://")
	trimmed = strings.TrimPrefix(trimmed, "file://")
	trimmed = strings.TrimPrefix(trimmed, "#")
	trimmed = strings.Trim(trimmed, "/")
	trimmed = strings.ReplaceAll(trimmed, "/", "_")
	trimmed = strings.ReplaceAll(trimmed, ".", "_")
	trimmed = strings.ReplaceAll(trimmed, ":", "_")
	trimmed = strings.ReplaceAll(trimmed, "-", "_")
	trimmed = strings.ReplaceAll(trimmed, "#", "_")
	trimmed = strings.ReplaceAll(trimmed, " ", "_")
	trimmed = strings.ReplaceAll(trimmed, "\\", "_")
	trimmed = strings.ReplaceAll(trimmed, "?", "_")
	trimmed = strings.ReplaceAll(trimmed, "=", "_")
	trimmed = strings.ReplaceAll(trimmed, "&", "_")
	trimmed = strings.ReplaceAll(trimmed, "%", "_")
	trimmed = strings.ReplaceAll(trimmed, "$", "_")
	trimmed = strings.ReplaceAll(trimmed, "@", "_")
	trimmed = strings.ReplaceAll(trimmed, "+", "_")
	trimmed = strings.ReplaceAll(trimmed, "*", "_")
	trimmed = strings.ReplaceAll(trimmed, ",", "_")
	trimmed = strings.ReplaceAll(trimmed, "(", "_")
	trimmed = strings.ReplaceAll(trimmed, ")", "_")
	trimmed = strings.Trim(trimmed, "_")
	if trimmed == "" {
		trimmed = "ref"
	}
	parts := strings.Split(trimmed, "_")
	if len(parts) > 6 {
		parts = parts[len(parts)-6:]
	}
	return strings.Join(parts, "_")
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
