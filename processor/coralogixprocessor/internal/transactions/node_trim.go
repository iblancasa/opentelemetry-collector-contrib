// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package transactions // import "github.com/open-telemetry/opentelemetry-collector-contrib/processor/coralogixprocessor/internal/transactions"

import (
	"container/heap"

	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/ptrace"
)

// DefaultMaxTxnTraceNodes matches go-agent v3/newrelic/limits.go maxTxnTraceNodes.
const DefaultMaxTxnTraceNodes = 256

type durationHeapItem struct {
	duration int64
	index    int
	spanID   pcommon.SpanID
}

type durationMinHeap []durationHeapItem

func (h durationMinHeap) Len() int           { return len(h) }
func (h durationMinHeap) Less(i, j int) bool { return h[i].duration < h[j].duration }
func (h durationMinHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *durationMinHeap) Push(x any)        { *h = append(*h, x.(durationHeapItem)) }
func (h *durationMinHeap) Pop() any {
	old := *h
	n := len(old)
	item := old[n-1]
	*h = old[:n-1]
	return item
}

// TrimToSlowestNodes keeps at most maxNodes spans, preferring longer durations.
// The transaction root is never evicted. Dropped middle parents are rewritten so
// each kept span's parent_span_id points at the nearest kept ancestor (or empty).
// maxNodes <= 0 disables trimming. Returns the kept span IDs.
func TrimToSlowestNodes(spans []ptrace.Span, rootSpanID pcommon.SpanID, maxNodes int) map[pcommon.SpanID]struct{} {
	kept := make(map[pcommon.SpanID]struct{}, len(spans))
	if maxNodes <= 0 || len(spans) <= maxNodes {
		for _, span := range spans {
			kept[span.SpanID()] = struct{}{}
		}
		return kept
	}

	byID := make(map[pcommon.SpanID]ptrace.Span, len(spans))
	var others []ptrace.Span
	var root ptrace.Span
	rootFound := false
	for _, span := range spans {
		byID[span.SpanID()] = span
		if !rootSpanID.IsEmpty() && span.SpanID() == rootSpanID {
			root = span
			rootFound = true
			continue
		}
		others = append(others, span)
	}

	slots := maxNodes
	if rootFound {
		slots = maxNodes - 1
	}
	if slots <= 0 {
		if rootFound {
			kept[root.SpanID()] = struct{}{}
			reparentKeptSpans(spans, kept, byID)
		}
		return kept
	}

	h := &durationMinHeap{}
	for i, span := range others {
		duration := spanDurationNS(span)
		item := durationHeapItem{duration: duration, index: i, spanID: span.SpanID()}
		if h.Len() < slots {
			heap.Push(h, item)
			continue
		}
		if duration > (*h)[0].duration {
			heap.Pop(h)
			heap.Push(h, item)
		}
	}

	for _, item := range *h {
		kept[item.spanID] = struct{}{}
	}
	if rootFound {
		kept[root.SpanID()] = struct{}{}
	}

	reparentKeptSpans(spans, kept, byID)
	return kept
}

func spanDurationNS(span ptrace.Span) int64 {
	start := int64(span.StartTimestamp())
	end := int64(span.EndTimestamp())
	if end < start {
		return 0
	}
	return end - start
}

func reparentKeptSpans(all []ptrace.Span, kept map[pcommon.SpanID]struct{}, byID map[pcommon.SpanID]ptrace.Span) {
	for _, span := range all {
		if _, ok := kept[span.SpanID()]; !ok {
			continue
		}
		newParent := nearestKeptAncestor(span.ParentSpanID(), kept, byID)
		if newParent != span.ParentSpanID() {
			span.SetParentSpanID(newParent)
		}
	}
}

func nearestKeptAncestor(parentID pcommon.SpanID, kept map[pcommon.SpanID]struct{}, byID map[pcommon.SpanID]ptrace.Span) pcommon.SpanID {
	for !parentID.IsEmpty() {
		if _, ok := kept[parentID]; ok {
			return parentID
		}
		ancestor, ok := byID[parentID]
		if !ok {
			break
		}
		parentID = ancestor.ParentSpanID()
	}
	return pcommon.SpanID{}
}
