// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package spanmetricsconnector // import "github.com/open-telemetry/opentelemetry-collector-contrib/connector/spanmetricsconnector"

import (
	"slices"

	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/ptrace"
)

type selfTimeSpanNode struct {
	spanID   pcommon.SpanID
	parentID pcommon.SpanID
	startNS  int64
	endNS    int64
	children []*selfTimeSpanNode
}

// computeSelfTimeBySpanID builds per-trace trees from the batch and returns
// exclusive self-time in nanoseconds keyed by span ID. Partial traces (duplicate
// span IDs, or empty-parent roots alongside unresolved parents) are omitted.
// Requires complete traces in the batch (typically via groupbytrace).
func computeSelfTimeBySpanID(traces ptrace.Traces) map[pcommon.SpanID]int64 {
	spansByTrace := groupSpansByTraceID(traces)
	out := make(map[pcommon.SpanID]int64)
	for _, spans := range spansByTrace {
		for spanID, selfTime := range computeSelfTimeForTrace(spans) {
			out[spanID] = selfTime
		}
	}
	return out
}

func groupSpansByTraceID(traces ptrace.Traces) map[pcommon.TraceID][]ptrace.Span {
	grouped := make(map[pcommon.TraceID][]ptrace.Span)
	for i := 0; i < traces.ResourceSpans().Len(); i++ {
		rs := traces.ResourceSpans().At(i)
		for j := 0; j < rs.ScopeSpans().Len(); j++ {
			ss := rs.ScopeSpans().At(j)
			for k := 0; k < ss.Spans().Len(); k++ {
				span := ss.Spans().At(k)
				tid := span.TraceID()
				if tid.IsEmpty() {
					continue
				}
				grouped[tid] = append(grouped[tid], span)
			}
		}
	}
	return grouped
}

func computeSelfTimeForTrace(spans []ptrace.Span) map[pcommon.SpanID]int64 {
	nodes := make(map[pcommon.SpanID]*selfTimeSpanNode, len(spans))
	duplicates := make(map[pcommon.SpanID]struct{})
	for _, span := range spans {
		spanID := span.SpanID()
		if spanID.IsEmpty() {
			continue
		}
		if _, exists := nodes[spanID]; exists {
			duplicates[spanID] = struct{}{}
			continue
		}
		nodes[spanID] = &selfTimeSpanNode{
			spanID:   spanID,
			parentID: span.ParentSpanID(),
			startNS:  int64(span.StartTimestamp()),
			endNS:    int64(span.EndTimestamp()),
		}
	}

	missingParents := make(map[pcommon.SpanID]struct{})
	var hasExplicitRoot bool
	for _, node := range nodes {
		if node.parentID.IsEmpty() {
			hasExplicitRoot = true
			continue
		}
		parent, ok := nodes[node.parentID]
		if !ok {
			missingParents[node.parentID] = struct{}{}
			continue
		}
		parent.children = append(parent.children, node)
	}

	// Partial: ambiguous duplicates, or nominal root(s) with unresolved parents.
	if len(duplicates) > 0 || (hasExplicitRoot && len(missingParents) > 0) {
		return nil
	}

	out := make(map[pcommon.SpanID]int64, len(nodes))
	for spanID, node := range nodes {
		if selfTime, ok := selfTimeNS(node); ok {
			out[spanID] = selfTime
		}
	}
	return out
}

func selfTimeNS(node *selfTimeSpanNode) (int64, bool) {
	if node.endNS < node.startNS {
		return 0, false
	}
	duration := node.endNS - node.startNS
	if duration == 0 || len(node.children) == 0 {
		return duration, true
	}
	covered := coveredDurationNS(node.startNS, node.endNS, node.children)
	selfTime := duration - covered
	if selfTime < 0 {
		selfTime = 0
	}
	return selfTime, true
}

func coveredDurationNS(parentStart, parentEnd int64, children []*selfTimeSpanNode) int64 {
	type interval struct {
		start, end int64
	}
	clamped := make([]interval, 0, len(children))
	for _, child := range children {
		if child.endNS <= child.startNS {
			continue
		}
		start := max(child.startNS, parentStart)
		end := min(child.endNS, parentEnd)
		if end > start {
			clamped = append(clamped, interval{start: start, end: end})
		}
	}
	if len(clamped) == 0 {
		return 0
	}

	slices.SortFunc(clamped, func(a, b interval) int {
		if a.start != b.start {
			if a.start < b.start {
				return -1
			}
			return 1
		}
		if a.end < b.end {
			return -1
		}
		if a.end > b.end {
			return 1
		}
		return 0
	})

	mergedStart, mergedEnd := clamped[0].start, clamped[0].end
	var covered int64
	for _, iv := range clamped[1:] {
		if iv.start <= mergedEnd {
			if iv.end > mergedEnd {
				mergedEnd = iv.end
			}
			continue
		}
		covered += mergedEnd - mergedStart
		mergedStart, mergedEnd = iv.start, iv.end
	}
	covered += mergedEnd - mergedStart
	return covered
}
