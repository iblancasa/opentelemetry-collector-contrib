// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package transactions

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"go.uber.org/zap"
)

func TestTrimToSlowestNodes_KeepsLongestAndAlwaysRoot(t *testing.T) {
	root := newTrimSpan(1, 0, 200, 0, "root")
	auth := newTrimSpan(2, 1, 6, 1, "auth")     // 5
	cache := newTrimSpan(3, 10, 12, 1, "cache")  // 2
	db := newTrimSpan(4, 20, 60, 1, "db")        // 40
	http := newTrimSpan(5, 70, 150, 1, "http")   // 80
	render := newTrimSpan(6, 160, 170, 1, "render") // 10

	kept := TrimToSlowestNodes(
		[]ptrace.Span{root, auth, cache, db, http, render},
		root.SpanID(),
		3,
	)
	names := keptNames([]ptrace.Span{root, auth, cache, db, http, render}, kept)
	assert.Equal(t, map[string]struct{}{"root": {}, "db": {}, "http": {}}, names)
}

func TestTrimToSlowestNodes_ReparentsWhenMiddleParentDropped(t *testing.T) {
	root := newTrimSpan(1, 0, 100, 0, "root")
	mid := newTrimSpan(2, 1, 2, 1, "middleware") // 1
	db := newTrimSpan(3, 5, 90, 2, "db")          // 85

	kept := TrimToSlowestNodes([]ptrace.Span{root, mid, db}, root.SpanID(), 2)
	require.Len(t, kept, 2)
	_, midKept := kept[mid.SpanID()]
	assert.False(t, midKept)
	assert.Equal(t, root.SpanID(), db.ParentSpanID())
}

func TestTrimToSlowestNodes_DisabledWhenMaxNodesZero(t *testing.T) {
	root := newTrimSpan(1, 0, 100, 0, "root")
	child := newTrimSpan(2, 10, 20, 1, "child")
	kept := TrimToSlowestNodes([]ptrace.Span{root, child}, root.SpanID(), 0)
	assert.Len(t, kept, 2)
}

func TestHarvestHeap_CapacityOneKeepsSlower(t *testing.T) {
	heap := NewRegularTraceHeap(1)
	fast := HarvestTrace{DurationNS: 100, Traces: singleSpanTraces("fast", 100)}
	slow := HarvestTrace{DurationNS: 500, Traces: singleSpanTraces("slow", 500)}
	mid := HarvestTrace{DurationNS: 200, Traces: singleSpanTraces("mid", 200)}

	assert.True(t, heap.Witness(fast))
	assert.True(t, heap.Witness(mid))
	assert.True(t, heap.Witness(slow))
	winners := heap.Drain()
	require.Len(t, winners, 1)
	assert.Equal(t, int64(500), winners[0].DurationNS)
	assert.Equal(t, "slow", winners[0].Traces.ResourceSpans().At(0).ScopeSpans().At(0).Spans().At(0).Name())
}

func TestHarvestHeap_RejectsFasterThanWinner(t *testing.T) {
	heap := NewRegularTraceHeap(1)
	slow := HarvestTrace{DurationNS: 500, Traces: singleSpanTraces("slow", 500)}
	fast := HarvestTrace{DurationNS: 50, Traces: singleSpanTraces("fast", 50)}
	assert.True(t, heap.Witness(slow))
	assert.False(t, heap.Witness(fast))
	assert.Equal(t, "slow", heap.Drain()[0].Traces.ResourceSpans().At(0).ScopeSpans().At(0).Spans().At(0).Name())
}

func TestPartitionedHarvest_ServicesDoNotCompete(t *testing.T) {
	heap := NewPartitionedRegularTraceHeap(1)

	assert.True(t, heap.Witness(HarvestTrace{
		DurationNS: 100,
		Partition:  "api",
		Traces:     singleSpanTraces("api-fast", 100),
	}))
	assert.True(t, heap.Witness(HarvestTrace{
		DurationNS: 500,
		Partition:  "api",
		Traces:     singleSpanTraces("api-slow", 500),
	}))
	assert.True(t, heap.Witness(HarvestTrace{
		DurationNS: 50,
		Partition:  "worker",
		Traces:     singleSpanTraces("worker-only", 50),
	}))

	winners := heap.Drain()
	require.Len(t, winners, 2)
	names := map[string]struct{}{}
	for _, w := range winners {
		names[w.Traces.ResourceSpans().At(0).ScopeSpans().At(0).Spans().At(0).Name()] = struct{}{}
	}
	assert.Equal(t, map[string]struct{}{
		"api-slow":     {},
		"worker-only": {},
	}, names)
}

func TestSelfTimeComputedOnFullTreeBeforeTrim(t *testing.T) {
	// parent [0,100], overlapping children [10,30] U [20,50] => self=60 on full tree
	// Cap=2 keeps parent + longest child (40ns child B); self-time attrs still from full tree.
	parent := newTrimSpan(1, 0, 100, 0, "parent")
	childA := newTrimSpan(2, 10, 30, 1, "a") // 20
	childB := newTrimSpan(3, 20, 50, 1, "b") // 30

	processed := ProcessCompletedTrace(
		[]ptrace.Span{parent, childA, childB},
		zapNop(),
		ProcessOptions{MaxNodes: 2},
	)

	val, ok := parent.Attributes().Get(TransactionSelfTime)
	require.True(t, ok)
	assert.Equal(t, int64(60), val.Int())

	require.Len(t, processed.KeptIDs, 2)
	_, keptA := processed.KeptIDs[childA.SpanID()]
	_, keptB := processed.KeptIDs[childB.SpanID()]
	assert.False(t, keptA)
	assert.True(t, keptB)
}

func newTrimSpan(spanID byte, startNS, endNS int64, parentSpanID byte, name string) ptrace.Span {
	span := ptrace.NewSpan()
	span.SetTraceID(pcommon.TraceID([16]byte{1}))
	span.SetSpanID(pcommon.SpanID([8]byte{spanID}))
	if parentSpanID != 0 {
		span.SetParentSpanID(pcommon.SpanID([8]byte{parentSpanID}))
	}
	span.SetStartTimestamp(pcommon.Timestamp(startNS))
	span.SetEndTimestamp(pcommon.Timestamp(endNS))
	span.SetName(name)
	return span
}

func keptNames(spans []ptrace.Span, kept map[pcommon.SpanID]struct{}) map[string]struct{} {
	out := make(map[string]struct{})
	for _, span := range spans {
		if _, ok := kept[span.SpanID()]; ok {
			out[span.Name()] = struct{}{}
		}
	}
	return out
}

func singleSpanTraces(name string, durationNS int64) ptrace.Traces {
	td := ptrace.NewTraces()
	span := td.ResourceSpans().AppendEmpty().ScopeSpans().AppendEmpty().Spans().AppendEmpty()
	span.SetName(name)
	span.SetStartTimestamp(0)
	span.SetEndTimestamp(pcommon.Timestamp(durationNS))
	span.SetSpanID(pcommon.SpanID([8]byte{1}))
	span.SetTraceID(pcommon.TraceID([16]byte{1}))
	return td
}

func zapNop() *zap.Logger {
	return zap.NewNop()
}
