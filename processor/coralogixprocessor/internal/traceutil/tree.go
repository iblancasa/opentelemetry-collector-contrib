// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package traceutil // import "github.com/open-telemetry/opentelemetry-collector-contrib/processor/coralogixprocessor/internal/traceutil"

import (
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/ptrace"
)

type TraceTreeNode struct {
	Span     ptrace.Span
	Parent   *TraceTreeNode
	Children []*TraceTreeNode
	StartNS  int64
	EndNS    int64
}

type TraceTree struct {
	Roots            []*TraceTreeNode
	Nodes            map[pcommon.SpanID]*TraceTreeNode
	MissingParentIDs map[pcommon.SpanID]struct{}
	DuplicateSpanIDs map[pcommon.SpanID]struct{}
}

func BuildTraceTree(spans []ptrace.Span) TraceTree {
	tree := TraceTree{
		Nodes:            make(map[pcommon.SpanID]*TraceTreeNode, len(spans)),
		MissingParentIDs: make(map[pcommon.SpanID]struct{}),
		DuplicateSpanIDs: make(map[pcommon.SpanID]struct{}),
	}
	for _, span := range spans {
		spanID := span.SpanID()
		if _, exists := tree.Nodes[spanID]; exists {
			tree.DuplicateSpanIDs[spanID] = struct{}{}
			continue
		}
		tree.Nodes[spanID] = &TraceTreeNode{
			Span:    span,
			StartNS: int64(span.StartTimestamp()),
			EndNS:   int64(span.EndTimestamp()),
		}
	}

	for _, node := range tree.Nodes {
		parentID := node.Span.ParentSpanID()
		parent, ok := tree.Nodes[parentID]
		if parentID.IsEmpty() {
			tree.Roots = append(tree.Roots, node)
			continue
		}
		if !ok {
			tree.MissingParentIDs[parentID] = struct{}{}
			tree.Roots = append(tree.Roots, node)
			continue
		}
		node.Parent = parent
		parent.Children = append(parent.Children, node)
	}

	return tree
}
