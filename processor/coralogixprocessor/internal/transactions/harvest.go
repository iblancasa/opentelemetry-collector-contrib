// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package transactions // import "github.com/open-telemetry/opentelemetry-collector-contrib/processor/coralogixprocessor/internal/transactions"

import (
	"container/heap"
	"sort"
	"strings"

	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/ptrace"
)

// DefaultMaxRegularTraces matches go-agent maxRegularTraces.
const DefaultMaxRegularTraces = 1

// HarvestTrace is one completed, already-trimmed local trace competing for export.
type HarvestTrace struct {
	DurationNS int64
	// Partition groups peers that should compete. See HarvestPartitionKey.
	Partition string
	Traces    ptrace.Traces
}

type harvestMinHeap []HarvestTrace

func (h harvestMinHeap) Len() int           { return len(h) }
func (h harvestMinHeap) Less(i, j int) bool { return h[i].DurationNS < h[j].DurationNS }
func (h harvestMinHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *harvestMinHeap) Push(x any)        { *h = append(*h, x.(HarvestTrace)) }
func (h *harvestMinHeap) Pop() any {
	old := *h
	n := len(old)
	item := old[n-1]
	*h = old[:n-1]
	return item
}

// RegularTraceHeap keeps at most maxTraces completed traces by root duration
// within a single partition.
type RegularTraceHeap struct {
	maxTraces int
	heap      harvestMinHeap
}

func NewRegularTraceHeap(maxTraces int) *RegularTraceHeap {
	return &RegularTraceHeap{maxTraces: maxTraces}
}

func (h *RegularTraceHeap) MaxTraces() int { return h.maxTraces }

func (h *RegularTraceHeap) Len() int { return h.heap.Len() }

// Witness offers a completed trace. Returns true if it was kept.
func (h *RegularTraceHeap) Witness(trace HarvestTrace) bool {
	if h.maxTraces <= 0 {
		return false
	}
	if h.heap.Len() < h.maxTraces {
		heap.Push(&h.heap, trace)
		return true
	}
	if trace.DurationNS <= h.heap[0].DurationNS {
		return false
	}
	heap.Pop(&h.heap)
	heap.Push(&h.heap, trace)
	return true
}

// Drain removes and returns all kept traces (order not significant).
func (h *RegularTraceHeap) Drain() []HarvestTrace {
	out := make([]HarvestTrace, len(h.heap))
	copy(out, h.heap)
	h.heap = h.heap[:0]
	return out
}

// PartitionedRegularTraceHeap keeps a separate RegularTraceHeap per partition
// key. Different keys do not compete (see HarvestPartitionKey).
type PartitionedRegularTraceHeap struct {
	maxTraces  int
	partitions map[string]*RegularTraceHeap
}

func NewPartitionedRegularTraceHeap(maxTraces int) *PartitionedRegularTraceHeap {
	return &PartitionedRegularTraceHeap{
		maxTraces:  maxTraces,
		partitions: make(map[string]*RegularTraceHeap),
	}
}

func (h *PartitionedRegularTraceHeap) MaxTraces() int { return h.maxTraces }

// Witness offers a completed trace into its partition heap.
func (h *PartitionedRegularTraceHeap) Witness(trace HarvestTrace) bool {
	if h.maxTraces <= 0 {
		return false
	}
	part := h.partitions[trace.Partition]
	if part == nil {
		part = NewRegularTraceHeap(h.maxTraces)
		h.partitions[trace.Partition] = part
	}
	return part.Witness(trace)
}

// Drain removes and returns winners from every partition.
func (h *PartitionedRegularTraceHeap) Drain() []HarvestTrace {
	var out []HarvestTrace
	for key, part := range h.partitions {
		out = append(out, part.Drain()...)
		delete(h.partitions, key)
	}
	return out
}

// RootDurationNS returns the transaction root duration, else the max span duration.
func RootDurationNS(spans []ptrace.Span) int64 {
	var maxDuration int64
	var rootDuration int64
	rootFound := false
	for _, span := range spans {
		duration := spanDurationNS(span)
		if duration > maxDuration {
			maxDuration = duration
		}
		if _, ok := span.Attributes().Get(TransactionIdentifierRoot); ok {
			rootDuration = duration
			rootFound = true
		}
	}
	if rootFound {
		return rootDuration
	}
	return maxDuration
}

// ServiceNameForSpan returns resource service.name for the given span ID in td.
// Empty string if unknown (still a valid partition key).
func ServiceNameForSpan(td ptrace.Traces, spanID pcommon.SpanID) string {
	for i := 0; i < td.ResourceSpans().Len(); i++ {
		rs := td.ResourceSpans().At(i)
		svc := ""
		if v, ok := rs.Resource().Attributes().Get("service.name"); ok {
			svc = v.Str()
		}
		for j := 0; j < rs.ScopeSpans().Len(); j++ {
			ss := rs.ScopeSpans().At(j)
			for k := 0; k < ss.Spans().Len(); k++ {
				if ss.Spans().At(k).SpanID() == spanID {
					return svc
				}
			}
		}
	}
	return ""
}

// TransactionNameForSpan returns cgx.transaction on the span, else the span name.
func TransactionNameForSpan(td ptrace.Traces, spanID pcommon.SpanID) string {
	for i := 0; i < td.ResourceSpans().Len(); i++ {
		rs := td.ResourceSpans().At(i)
		for j := 0; j < rs.ScopeSpans().Len(); j++ {
			ss := rs.ScopeSpans().At(j)
			for k := 0; k < ss.Spans().Len(); k++ {
				span := ss.Spans().At(k)
				if span.SpanID() != spanID {
					continue
				}
				if v, ok := span.Attributes().Get(TransactionIdentifier); ok {
					return v.Str()
				}
				return span.Name()
			}
		}
	}
	return ""
}

// DistinctServiceNames returns sorted unique service.name values among kept spans
// (or every span when keptIDs is nil).
func DistinctServiceNames(td ptrace.Traces, keptIDs map[pcommon.SpanID]struct{}) []string {
	seen := make(map[string]struct{})
	for i := 0; i < td.ResourceSpans().Len(); i++ {
		rs := td.ResourceSpans().At(i)
		svc := ""
		if v, ok := rs.Resource().Attributes().Get("service.name"); ok {
			svc = v.Str()
		}
		for j := 0; j < rs.ScopeSpans().Len(); j++ {
			ss := rs.ScopeSpans().At(j)
			for k := 0; k < ss.Spans().Len(); k++ {
				span := ss.Spans().At(k)
				if keptIDs != nil {
					if _, ok := keptIDs[span.SpanID()]; !ok {
						continue
					}
				}
				seen[svc] = struct{}{}
			}
		}
	}
	out := make([]string, 0, len(seen))
	for svc := range seen {
		out = append(out, svc)
	}
	sort.Strings(out)
	return out
}

// HarvestPartitionKey builds the harvest competition bucket.
//
// Traces compete only when they share:
//  1. transaction-root service.name
//  2. transaction name (cgx.transaction / root span name)
//  3. the set of service.names present in the (kept) trace
//
// So api→worker and api→db do not compete, even when both roots are service
// "api". Different transaction names on the same service path also do not
// compete.
func HarvestPartitionKey(td ptrace.Traces, rootSpanID pcommon.SpanID, keptIDs map[pcommon.SpanID]struct{}) string {
	rootSvc := ServiceNameForSpan(td, rootSpanID)
	txn := TransactionNameForSpan(td, rootSpanID)
	peers := DistinctServiceNames(td, keptIDs)
	var b strings.Builder
	b.WriteString(rootSvc)
	b.WriteByte(0)
	b.WriteString(txn)
	b.WriteByte(0)
	b.WriteString(strings.Join(peers, "\x00"))
	return b.String()
}
