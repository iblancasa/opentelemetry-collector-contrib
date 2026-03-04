// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGenerateBundle(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	coreRoot := filepath.Join(tmp, "core")
	contribRoot := filepath.Join(tmp, "contrib")

	require.NoError(t, os.MkdirAll(filepath.Join(coreRoot, "exporter", "debugexporter"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(coreRoot, "exporter", "debugexporter", "config.schema.yaml"), []byte(`
type: object
`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(coreRoot, "exporter", "debugexporter", "metadata.yaml"), []byte(`
type: debug
status:
  class: exporter
`), 0o644))

	require.NoError(t, os.MkdirAll(filepath.Join(contribRoot, "receiver", "netflowreceiver"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(contribRoot, "receiver", "netflowreceiver", "config.schema.yaml"), []byte(`
type: object
`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(contribRoot, "receiver", "netflowreceiver", "metadata.yaml"), []byte(`
type: netflow
status:
  class: receiver
`), 0o644))

	config := filepath.Join(tmp, "config.yaml")
	require.NoError(t, os.WriteFile(config, []byte(`
receivers:
  netflow: {}
exporters:
  debug: {}
`), 0o644))

	out := filepath.Join(tmp, "bundle.json")
	err := GenerateBundle(config, out, "json", map[string]string{
		"go.opentelemetry.io/collector":                             coreRoot,
		"github.com/open-telemetry/opentelemetry-collector-contrib": contribRoot,
	}, false, false, false)
	require.NoError(t, err)

	raw, err := os.ReadFile(out)
	require.NoError(t, err)

	var doc map[string]any
	require.NoError(t, json.Unmarshal(raw, &doc))

	props := doc["properties"].(map[string]any)
	receivers := props["receivers"].(map[string]any)
	receiverProps := receivers["properties"].(map[string]any)
	netflow := receiverProps["netflow"].(map[string]any)
	require.Equal(t, "github.com/open-telemetry/opentelemetry-collector-contrib/receiver/netflowreceiver", netflow["$ref"])

	exporters := props["exporters"].(map[string]any)
	exporterProps := exporters["properties"].(map[string]any)
	debug := exporterProps["debug"].(map[string]any)
	require.Equal(t, "go.opentelemetry.io/collector/exporter/debugexporter", debug["$ref"])

	service := props["service"].(map[string]any)
	serviceProps := service["properties"].(map[string]any)
	extensions := serviceProps["extensions"].(map[string]any)
	require.Equal(t, "array", extensions["type"])
	pipelines := serviceProps["pipelines"].(map[string]any)
	require.Equal(t, "object", pipelines["type"])
}
