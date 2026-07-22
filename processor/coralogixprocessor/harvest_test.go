// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package coralogixprocessor

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/consumer/consumertest"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"go.opentelemetry.io/collector/processor/processortest"
	"go.uber.org/zap"

	"github.com/open-telemetry/opentelemetry-collector-contrib/processor/coralogixprocessor/internal/transactions"
)

func TestHarvest_FasterTraceNeverForwardedUntilFlushKeepsSlower(t *testing.T) {
	sink := new(consumertest.TracesSink)
	sp := &coralogixProcessor{
		config: &Config{
			TransactionsConfig: TransactionsConfig{
				Enabled:          true,
				MaxNodes:         0,
				MaxRegularTraces: 1,
				HarvestPeriod:    time.Hour,
			},
		},
		nextConsumer: sink,
		logger:       zap.NewNop(),
		harvest:      transactions.NewPartitionedRegularTraceHeap(1),
		harvestStop:  make(chan struct{}),
		harvestDone:  make(chan struct{}),
	}

	// Same partition: same service + transaction name + peer set.
	fast := newNamedRootTrace("checkout", "POST /pay", 100)
	slow := newNamedRootTrace("checkout", "POST /pay", 500)

	out, err := sp.processTraces(context.Background(), fast)
	require.NoError(t, err)
	assert.Equal(t, 0, out.SpanCount())
	assert.Equal(t, 0, sink.SpanCount())

	out, err = sp.processTraces(context.Background(), slow)
	require.NoError(t, err)
	assert.Equal(t, 0, out.SpanCount())
	assert.Equal(t, 0, sink.SpanCount())

	sp.flushHarvest(context.Background())
	require.Len(t, sink.AllTraces(), 1)
	got := sink.AllTraces()[0].ResourceSpans().At(0).ScopeSpans().At(0).Spans().At(0)
	assert.Equal(t, "POST /pay", got.Name())
	assert.Equal(t, pcommon.Timestamp(500), got.EndTimestamp())
}

func TestHarvest_PartitionedByServiceName(t *testing.T) {
	sink := new(consumertest.TracesSink)
	sp := &coralogixProcessor{
		config: &Config{
			TransactionsConfig: TransactionsConfig{
				Enabled:          true,
				MaxNodes:         0,
				MaxRegularTraces: 1,
				HarvestPeriod:    time.Hour,
			},
		},
		nextConsumer: sink,
		logger:       zap.NewNop(),
		harvest:      transactions.NewPartitionedRegularTraceHeap(1),
		harvestStop:  make(chan struct{}),
		harvestDone:  make(chan struct{}),
	}

	_, err := sp.processTraces(context.Background(), newNamedRootTrace("api", "GET /x", 100))
	require.NoError(t, err)
	_, err = sp.processTraces(context.Background(), newNamedRootTrace("api", "GET /x", 500))
	require.NoError(t, err)
	_, err = sp.processTraces(context.Background(), newNamedRootTrace("worker", "job", 50))
	require.NoError(t, err)

	assert.Equal(t, 0, sink.SpanCount())
	sp.flushHarvest(context.Background())

	require.Len(t, sink.AllTraces(), 2)
	byService := map[string]pcommon.Timestamp{}
	for _, td := range sink.AllTraces() {
		svc := td.ResourceSpans().At(0).Resource().Attributes().AsRaw()["service.name"].(string)
		span := td.ResourceSpans().At(0).ScopeSpans().At(0).Spans().At(0)
		byService[svc] = span.EndTimestamp()
	}
	assert.Equal(t, map[string]pcommon.Timestamp{
		"api":    500,
		"worker": 50,
	}, byService)
}

func TestHarvest_ApiWorkerDoesNotCompeteWithApiDb(t *testing.T) {
	sink := new(consumertest.TracesSink)
	sp := &coralogixProcessor{
		config: &Config{
			TransactionsConfig: TransactionsConfig{
				Enabled:          true,
				MaxNodes:         0,
				MaxRegularTraces: 1,
				HarvestPeriod:    time.Hour,
			},
		},
		nextConsumer: sink,
		logger:       zap.NewNop(),
		harvest:      transactions.NewPartitionedRegularTraceHeap(1),
		harvestStop:  make(chan struct{}),
		harvestDone:  make(chan struct{}),
	}

	// Same root service + same transaction name; different peer service sets.
	_, err := sp.processTraces(context.Background(), newRootWithPeer("api", "GET /orders", "worker", 100, 1, 2))
	require.NoError(t, err)
	_, err = sp.processTraces(context.Background(), newRootWithPeer("api", "GET /orders", "worker", 500, 3, 4))
	require.NoError(t, err)
	_, err = sp.processTraces(context.Background(), newRootWithPeer("api", "GET /orders", "db", 50, 5, 6))
	require.NoError(t, err)

	assert.Equal(t, 0, sink.SpanCount())
	sp.flushHarvest(context.Background())

	require.Len(t, sink.AllTraces(), 2)
	peerServices := map[string]pcommon.Timestamp{}
	for _, td := range sink.AllTraces() {
		var peer string
		var rootEnd pcommon.Timestamp
		for i := 0; i < td.ResourceSpans().Len(); i++ {
			rs := td.ResourceSpans().At(i)
			svc := rs.Resource().Attributes().AsRaw()["service.name"].(string)
			for j := 0; j < rs.ScopeSpans().Len(); j++ {
				for k := 0; k < rs.ScopeSpans().At(j).Spans().Len(); k++ {
					span := rs.ScopeSpans().At(j).Spans().At(k)
					if svc == "api" {
						rootEnd = span.EndTimestamp()
					} else {
						peer = svc
					}
				}
			}
		}
		peerServices[peer] = rootEnd
	}
	assert.Equal(t, map[string]pcommon.Timestamp{
		"worker": 500,
		"db":     50,
	}, peerServices)
}

func TestHarvest_DifferentTransactionNamesDoNotCompete(t *testing.T) {
	sink := new(consumertest.TracesSink)
	sp := &coralogixProcessor{
		config: &Config{
			TransactionsConfig: TransactionsConfig{
				Enabled:          true,
				MaxNodes:         0,
				MaxRegularTraces: 1,
				HarvestPeriod:    time.Hour,
			},
		},
		nextConsumer: sink,
		logger:       zap.NewNop(),
		harvest:      transactions.NewPartitionedRegularTraceHeap(1),
		harvestStop:  make(chan struct{}),
		harvestDone:  make(chan struct{}),
	}

	_, err := sp.processTraces(context.Background(), newNamedRootTrace("api", "GET /a", 100))
	require.NoError(t, err)
	_, err = sp.processTraces(context.Background(), newNamedRootTrace("api", "GET /b", 50))
	require.NoError(t, err)

	sp.flushHarvest(context.Background())
	require.Len(t, sink.AllTraces(), 2)
}

func TestFactory_CreatesWithHarvestDefaults(t *testing.T) {
	factory := NewFactory()
	cfg := factory.CreateDefaultConfig().(*Config)
	cfg.TransactionsConfig.Enabled = true
	sink := new(consumertest.TracesSink)
	proc, err := factory.CreateTraces(context.Background(), processortest.NewNopSettings(typ), cfg, sink)
	require.NoError(t, err)
	require.NoError(t, proc.Start(context.Background(), nil))
	require.NoError(t, proc.Shutdown(context.Background()))
}

func newNamedRootTrace(serviceName, spanName string, durationNS int64) ptrace.Traces {
	td := ptrace.NewTraces()
	rs := td.ResourceSpans().AppendEmpty()
	rs.Resource().Attributes().PutStr("service.name", serviceName)
	span := rs.ScopeSpans().AppendEmpty().Spans().AppendEmpty()
	span.SetName(spanName)
	span.SetKind(ptrace.SpanKindServer)
	span.SetTraceID(pcommon.TraceID([16]byte{1}))
	span.SetSpanID(pcommon.SpanID([8]byte{1}))
	span.SetStartTimestamp(0)
	span.SetEndTimestamp(pcommon.Timestamp(durationNS))
	return td
}

// newRootWithPeer builds api(root) → peer(client child under a different resource).
func newRootWithPeer(rootSvc, rootName, peerSvc string, rootDurationNS int64, rootID, peerID byte) ptrace.Traces {
	td := ptrace.NewTraces()
	traceID := pcommon.TraceID([16]byte{1})

	rootRS := td.ResourceSpans().AppendEmpty()
	rootRS.Resource().Attributes().PutStr("service.name", rootSvc)
	root := rootRS.ScopeSpans().AppendEmpty().Spans().AppendEmpty()
	root.SetName(rootName)
	root.SetKind(ptrace.SpanKindServer)
	root.SetTraceID(traceID)
	root.SetSpanID(pcommon.SpanID([8]byte{rootID}))
	root.SetStartTimestamp(0)
	root.SetEndTimestamp(pcommon.Timestamp(rootDurationNS))

	peerRS := td.ResourceSpans().AppendEmpty()
	peerRS.Resource().Attributes().PutStr("service.name", peerSvc)
	peer := peerRS.ScopeSpans().AppendEmpty().Spans().AppendEmpty()
	peer.SetName("peer-op")
	peer.SetKind(ptrace.SpanKindClient)
	peer.SetTraceID(traceID)
	peer.SetSpanID(pcommon.SpanID([8]byte{peerID}))
	peer.SetParentSpanID(root.SpanID())
	peer.SetStartTimestamp(0)
	peer.SetEndTimestamp(pcommon.Timestamp(rootDurationNS / 2))
	return td
}
