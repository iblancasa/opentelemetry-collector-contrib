// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package transactions // import "github.com/open-telemetry/opentelemetry-collector-contrib/processor/coralogixprocessor/internal/transactions"

import (
	"slices"

	"github.com/open-telemetry/opentelemetry-collector-contrib/processor/coralogixprocessor/internal/traceutil"
)

const TransactionSelfTime = "cgx.transaction.self_time_ns"

// applySelfTimeAttributes writes cgx.transaction.self_time_ns on every span in
// the tree when the trace is complete enough for an exact exclusive-time
// calculation. Partial traces (duplicate span IDs, or empty-parent roots
// alongside unresolved parents) are skipped. Must run on the full tree before
// node trimming.
func applySelfTimeAttributes(tree traceutil.TraceTree) {
	if isPartialTrace(tree) {
		return
	}
	for _, node := range tree.Nodes {
		selfTime, ok := computeSelfTimeNS(node)
		if !ok {
			continue
		}
		node.Span.Attributes().PutInt(TransactionSelfTime, selfTime)
	}
}

func isPartialTrace(tree traceutil.TraceTree) bool {
	if len(tree.DuplicateSpanIDs) > 0 {
		return true
	}
	if len(tree.MissingParentIDs) == 0 {
		return false
	}
	for _, root := range tree.Roots {
		if root.Span.ParentSpanID().IsEmpty() {
			return true
		}
	}
	return false
}

func computeSelfTimeNS(node *traceutil.TraceTreeNode) (int64, bool) {
	if node.EndNS < node.StartNS {
		return 0, false
	}
	duration := node.EndNS - node.StartNS
	if duration == 0 || len(node.Children) == 0 {
		return duration, true
	}
	covered := coveredDurationNS(node.StartNS, node.EndNS, node.Children)
	selfTime := duration - covered
	if selfTime < 0 {
		selfTime = 0
	}
	return selfTime, true
}

func coveredDurationNS(parentStart, parentEnd int64, children []*traceutil.TraceTreeNode) int64 {
	type interval struct {
		start, end int64
	}
	clamped := make([]interval, 0, len(children))
	for _, child := range children {
		if child.EndNS <= child.StartNS {
			continue
		}
		start := max(child.StartNS, parentStart)
		end := min(child.EndNS, parentEnd)
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
