// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidate_ModuleRef(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	coreRoot := filepath.Join(tmp, "core")

	schemaDir := filepath.Join(coreRoot, "config", "confighttp")
	require.NoError(t, os.MkdirAll(schemaDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(schemaDir, "config.schema.yaml"), []byte(`
$defs:
  client_config:
    type: object
    properties:
      timeout:
        type: string
type: object
`), 0o644))

	rootSchema := filepath.Join(tmp, "root.schema.yaml")
	require.NoError(t, os.WriteFile(rootSchema, []byte(`
type: object
properties:
  client:
    $ref: go.opentelemetry.io/collector/config/confighttp.client_config
`), 0o644))

	config := filepath.Join(tmp, "config.yaml")
	require.NoError(t, os.WriteFile(config, []byte(`
client:
  timeout: 5s
`), 0o644))

	err := Validate(rootSchema, config, map[string]string{
		"go.opentelemetry.io/collector": coreRoot,
	})
	require.NoError(t, err)
}

func TestValidate_ModuleRef_NoDef(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	coreRoot := filepath.Join(tmp, "core")

	schemaDir := filepath.Join(coreRoot, "receiver", "netflowreceiver")
	require.NoError(t, os.MkdirAll(schemaDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(schemaDir, "config.schema.yaml"), []byte(`
type: object
properties:
  port:
    type: integer
`), 0o644))

	rootSchema := filepath.Join(tmp, "root.schema.yaml")
	require.NoError(t, os.WriteFile(rootSchema, []byte(`
type: object
properties:
  receivers:
    type: object
    properties:
      netflow:
        $ref: go.opentelemetry.io/collector/receiver/netflowreceiver
`), 0o644))

	config := filepath.Join(tmp, "config.yaml")
	require.NoError(t, os.WriteFile(config, []byte(`
receivers:
  netflow:
    port: 2055
`), 0o644))

	err := Validate(rootSchema, config, map[string]string{
		"go.opentelemetry.io/collector": coreRoot,
	})
	require.NoError(t, err)
}

func TestValidate_RelativeDotRef(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	protoDir := filepath.Join(tmp, "protocol")
	require.NoError(t, os.MkdirAll(protoDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(protoDir, "config.schema.yaml"), []byte(`
$defs:
  timer_histogram_mapping:
    type: object
    properties:
      name:
        type: string
type: object
`), 0o644))

	rootSchema := filepath.Join(tmp, "config.schema.yaml")
	require.NoError(t, os.WriteFile(rootSchema, []byte(`
type: object
properties:
  mapping:
    $ref: ./protocol.timer_histogram_mapping
`), 0o644))

	config := filepath.Join(tmp, "config.yaml")
	require.NoError(t, os.WriteFile(config, []byte(`
mapping:
  name: foo
`), 0o644))

	err := Validate(rootSchema, config, nil)
	require.NoError(t, err)
}

func TestValidate_DurationFormat(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()

	schema := filepath.Join(tmp, "config.schema.yaml")
	require.NoError(t, os.WriteFile(schema, []byte(`
type: object
properties:
  timeout:
    type: string
    format: duration
  interval:
    type: string
    format: duration
`), 0o644))

	t.Run("valid durations", func(t *testing.T) {
		validDurations := []string{
			"1s",
			"1m",
			"1h",
			"100ms",
			"500ns",
			"1m30s",
			"2h45m",
			"1.5s",
		}

		for _, dur := range validDurations {
			t.Run(dur, func(t *testing.T) {
				config := filepath.Join(tmp, "config_"+dur+".yaml")
				require.NoError(t, os.WriteFile(config, []byte(`
timeout: `+dur+`
interval: 5s
`), 0o644))

				err := Validate(schema, config, nil)
				require.NoError(t, err, "duration %q should be valid", dur)
			})
		}
	})

	t.Run("invalid durations", func(t *testing.T) {
		config := filepath.Join(tmp, "config_invalid.yaml")
		require.NoError(t, os.WriteFile(config, []byte(`
timeout: invalid
interval: 5s
`), 0o644))

		err := Validate(schema, config, nil)
		require.Error(t, err, "invalid duration should fail validation")
	})
}
