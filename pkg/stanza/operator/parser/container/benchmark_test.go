// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package container

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/component/componenttest"
	"go.uber.org/zap"

	"github.com/open-telemetry/opentelemetry-collector-contrib/pkg/stanza/entry"
	"github.com/open-telemetry/opentelemetry-collector-contrib/pkg/stanza/operator"
)

// benchmarkOutput simulates per-call overhead so the benchmark captures the cost
// of splitting batches into per-entry calls downstream.
type benchmarkOutput struct{}

var benchSink uint64

func (*benchmarkOutput) CanOutput() bool              { return false }
func (*benchmarkOutput) CanProcess() bool             { return true }
func (*benchmarkOutput) ID() string                   { return "bench" }
func (*benchmarkOutput) Type() string                 { return "bench" }
func (*benchmarkOutput) Logger() *zap.Logger          { return zap.NewNop() }
func (*benchmarkOutput) Outputs() []operator.Operator { return nil }
func (*benchmarkOutput) GetOutputIDs() []string       { return nil }
func (*benchmarkOutput) SetOutputs([]operator.Operator) error {
	return nil
}
func (*benchmarkOutput) SetOutputIDs([]string) {}
func (*benchmarkOutput) Start(operator.Persister) error {
	return nil
}
func (*benchmarkOutput) Stop() error { return nil }
func (*benchmarkOutput) ProcessBatch(_ context.Context, entries []*entry.Entry) error {
	benchWork()
	benchConsumeN(len(entries))
	return nil
}

func (*benchmarkOutput) Process(context.Context, *entry.Entry) error {
	benchWork()
	benchConsumeN(1)
	return nil
}

func benchWork() {
	var sum uint64
	for i := range 4096 {
		sum += uint64(i)
	}
	atomic.AddUint64(&benchSink, sum)
}

func benchConsumeN(count int) {
	atomic.AddUint64(&benchSink, uint64(count))
}

func BenchmarkProcessBatch(b *testing.B) {
	const batchSize = 10000

	b.Run("Docker", func(b *testing.B) {
		cfg := NewConfig()
		cfg.Format = dockerFormat
		cfg.AddMetadataFromFilePath = false
		cfg.OutputIDs = []string{"bench"}

		op, err := cfg.Build(componenttest.NewNopTelemetrySettings())
		require.NoError(b, err)
		b.Cleanup(func() {
			require.NoError(b, op.Stop())
		})
		require.NoError(b, op.SetOutputs([]operator.Operator{&benchmarkOutput{}}))

		template := makeDockerEntries(batchSize)
		for b.Loop() {
			entries := copyEntries(template)
			require.NoError(b, op.ProcessBatch(b.Context(), entries))
		}
	})

	b.Run("Containerd", func(b *testing.B) {
		cfg := NewConfig()
		cfg.Format = containerdFormat
		cfg.AddMetadataFromFilePath = false
		cfg.OutputIDs = []string{"bench"}

		op, err := cfg.Build(componenttest.NewNopTelemetrySettings())
		require.NoError(b, err)
		b.Cleanup(func() {
			require.NoError(b, op.Stop())
		})
		require.NoError(b, op.SetOutputs([]operator.Operator{&benchmarkOutput{}}))

		template := makeContainerdEntries(batchSize)
		for b.Loop() {
			entries := copyEntries(template)
			require.NoError(b, op.ProcessBatch(b.Context(), entries))
		}
	})

	b.Run("DockerSplit", func(b *testing.B) {
		cfg := NewConfig()
		cfg.Format = dockerFormat
		cfg.AddMetadataFromFilePath = false
		cfg.OutputIDs = []string{"bench"}

		op, err := cfg.Build(componenttest.NewNopTelemetrySettings())
		require.NoError(b, err)
		b.Cleanup(func() {
			require.NoError(b, op.Stop())
		})
		require.NoError(b, op.SetOutputs([]operator.Operator{&benchmarkOutput{}}))

		template := makeDockerEntries(batchSize)
		for b.Loop() {
			entries := copyEntries(template)
			for _, ent := range entries {
				require.NoError(b, op.Process(b.Context(), ent))
			}
		}
	})

	b.Run("ContainerdSplit", func(b *testing.B) {
		cfg := NewConfig()
		cfg.Format = containerdFormat
		cfg.AddMetadataFromFilePath = false
		cfg.OutputIDs = []string{"bench"}

		op, err := cfg.Build(componenttest.NewNopTelemetrySettings())
		require.NoError(b, err)
		b.Cleanup(func() {
			require.NoError(b, op.Stop())
		})
		require.NoError(b, op.SetOutputs([]operator.Operator{&benchmarkOutput{}}))

		template := makeContainerdEntries(batchSize)
		for b.Loop() {
			entries := copyEntries(template)
			for _, ent := range entries {
				require.NoError(b, op.Process(b.Context(), ent))
			}
		}
	})
}

func makeDockerEntries(count int) []*entry.Entry {
	entries := make([]*entry.Entry, count)
	for i := range count {
		e := entry.New()
		e.Body = `{"log":"benchmark line","stream":"stdout","time":"2029-03-30T08:31:20.545Z"}`
		entries[i] = e
	}
	return entries
}

func makeContainerdEntries(count int) []*entry.Entry {
	entries := make([]*entry.Entry, count)
	for i := range count {
		e := entry.New()
		e.Body = "2029-03-30T08:31:20.545Z stdout F benchmark line"
		entries[i] = e
	}
	return entries
}

func copyEntries(entries []*entry.Entry) []*entry.Entry {
	cloned := make([]*entry.Entry, len(entries))
	for i, e := range entries {
		cloned[i] = e.Copy()
	}
	return cloned
}
