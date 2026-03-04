// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type mapFlag map[string]string

func (m *mapFlag) String() string {
	if m == nil {
		return ""
	}
	var parts []string
	for k, v := range *m {
		parts = append(parts, fmt.Sprintf("%s=%s", k, v))
	}
	return strings.Join(parts, ",")
}

func (m *mapFlag) Set(value string) error {
	parts := strings.SplitN(value, "=", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return fmt.Errorf("invalid mapping %q, expected prefix=path", value)
	}
	prefix := strings.TrimSpace(parts[0])
	path := strings.TrimSpace(parts[1])
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	if _, err := os.Stat(abs); err != nil {
		return fmt.Errorf("mapping path %q: %w", abs, err)
	}
	if *m == nil {
		*m = make(map[string]string)
	}
	(*m)[prefix] = abs
	return nil
}

func main() {
	var configPath string
	var outPath string
	var fileType string
	var includeAll bool
	var allowMissing bool
	var inline bool
	var roots mapFlag

	flag.StringVar(&configPath, "config", "", "Path to the collector config file (json|yaml)")
	flag.StringVar(&outPath, "out", "", "Output schema path")
	flag.StringVar(&fileType, "t", "", "Output format: json|yaml (defaults to extension)")
	flag.BoolVar(&includeAll, "all", false, "Include all component schemas found in mapped roots")
	flag.BoolVar(&allowMissing, "allow-missing", false, "Allow missing component schemas by using an empty object schema")
	flag.BoolVar(&inline, "inline", false, "Inline external $ref schemas into a self-contained bundle")
	flag.Var(&roots, "map", "Map module prefix to local root (repeatable), e.g. -map go.opentelemetry.io/collector=/path/to/collector")
	flag.Parse()

	if outPath == "" || (!includeAll && configPath == "") {
		fmt.Fprintln(os.Stderr, "out is required; config is required unless -all is set")
		flag.Usage()
		os.Exit(2)
	}

	if err := GenerateBundle(configPath, outPath, fileType, roots, includeAll, allowMissing, inline); err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
	fmt.Println("schema bundle written to", outPath)
}
