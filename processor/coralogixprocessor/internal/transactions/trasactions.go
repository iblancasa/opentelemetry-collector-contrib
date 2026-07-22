// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package transactions // import "github.com/open-telemetry/opentelemetry-collector-contrib/processor/coralogixprocessor/internal/transactions"

import (
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"go.uber.org/zap"

	"github.com/open-telemetry/opentelemetry-collector-contrib/processor/coralogixprocessor/internal/traceutil"
)

const (
	TransactionIdentifier     = "cgx.transaction"
	TransactionIdentifierRoot = "cgx.transaction.root"
)

// ProcessOptions controls post-annotation sampling for one completed trace.
type ProcessOptions struct {
	MaxNodes int
}

// ProcessedTrace is one annotated (+ optionally trimmed) local trace.
type ProcessedTrace struct {
	RootSpanID pcommon.SpanID
	Spans      []ptrace.Span
	DurationNS int64
	KeptIDs    map[pcommon.SpanID]struct{}
}

func ApplyTransactionsAttributesByTraceID(spansByTraceID map[pcommon.TraceID][]ptrace.Span, logger *zap.Logger) {
	applyTransactionsAttributesByTraceID(spansByTraceID, logger, ProcessOptions{})
}

func applyTransactionsAttributesByTraceID(spansByTraceID map[pcommon.TraceID][]ptrace.Span, logger *zap.Logger, opts ProcessOptions) {
	for traceID, spans := range spansByTraceID {
		if len(spans) == 0 {
			logger.Debug("skipping empty trace span group", zap.String("traceID", traceID.String()))
			continue
		}
		logger.Debug("processing trace", zap.String("traceID", traceID.String()), zap.Int("spans", len(spans)))
		ProcessCompletedTrace(spans, logger, opts)
	}
}

func ApplyTransactionAttributesToTree(tree traceutil.TraceTree, logger *zap.Logger) {
	ApplyTransactionAttributesToTreeWithOptions(tree, logger, ProcessOptions{})
}

func ApplyTransactionAttributesToTreeWithOptions(tree traceutil.TraceTree, logger *zap.Logger, opts ProcessOptions) ProcessedTrace {
	root := selectSpanRoot(tree, logger)
	var rootID pcommon.SpanID
	if root != nil {
		markSpanAsRoot(root.Span)
		applyTransactionToTrace(root, root.Span.Name())
		rootID = root.Span.SpanID()
	}
	// Self-time on the full tree before any node trim.
	applySelfTimeAttributes(tree)

	spans := make([]ptrace.Span, 0, len(tree.Nodes))
	for _, node := range tree.Nodes {
		spans = append(spans, node.Span)
	}
	kept := TrimToSlowestNodes(spans, rootID, opts.MaxNodes)
	duration := int64(0)
	if root != nil {
		duration = spanDurationNS(root.Span)
	} else {
		duration = RootDurationNS(spans)
	}
	return ProcessedTrace{
		RootSpanID: rootID,
		Spans:      spans,
		DurationNS: duration,
		KeptIDs:    kept,
	}
}

// ProcessCompletedTrace annotates transaction attrs + self-time on the full
// tree, then trims to the slowest max_nodes spans (always keeping the root).
func ProcessCompletedTrace(spans []ptrace.Span, logger *zap.Logger, opts ProcessOptions) ProcessedTrace {
	tree := traceutil.BuildTraceTree(spans)
	return ApplyTransactionAttributesToTreeWithOptions(tree, logger, opts)
}

func applyTransactionToTrace(currentSpan *traceutil.TraceTreeNode, transactionName string) {
	for _, child := range currentSpan.Children {
		if _, ok := child.Span.Attributes().Get(TransactionIdentifierRoot); ok {
			applyTransactionToTrace(child, child.Span.Name())
		} else if child.Span.Kind() == ptrace.SpanKindServer || child.Span.Kind() == ptrace.SpanKindConsumer {
			markSpanAsRoot(child.Span)
			applyTransactionToTrace(child, child.Span.Name())
		} else {
			child.Span.Attributes().PutStr(TransactionIdentifier, transactionName)
			applyTransactionToTrace(child, transactionName)
		}
	}
}

func markSpanAsRoot(span ptrace.Span) {
	transactionName := span.Name()
	span.Attributes().PutStr(TransactionIdentifier, transactionName)
	span.Attributes().PutBool(TransactionIdentifierRoot, true)
}

// CopyTraceKeepingSpans builds a new Traces containing only kept spans,
// preserving resource/scope grouping from the original batch.
func CopyTraceKeepingSpans(td ptrace.Traces, keep map[pcommon.SpanID]struct{}) ptrace.Traces {
	out := ptrace.NewTraces()
	if len(keep) == 0 {
		return out
	}
	for i := 0; i < td.ResourceSpans().Len(); i++ {
		rs := td.ResourceSpans().At(i)
		var outRS ptrace.ResourceSpans
		outRSInitialized := false
		for j := 0; j < rs.ScopeSpans().Len(); j++ {
			ss := rs.ScopeSpans().At(j)
			var outSS ptrace.ScopeSpans
			outSSInitialized := false
			for k := 0; k < ss.Spans().Len(); k++ {
				span := ss.Spans().At(k)
				if _, ok := keep[span.SpanID()]; !ok {
					continue
				}
				if !outRSInitialized {
					outRS = out.ResourceSpans().AppendEmpty()
					rs.Resource().CopyTo(outRS.Resource())
					outRSInitialized = true
				}
				if !outSSInitialized {
					outSS = outRS.ScopeSpans().AppendEmpty()
					ss.Scope().CopyTo(outSS.Scope())
					outSSInitialized = true
				}
				span.CopyTo(outSS.Spans().AppendEmpty())
			}
		}
	}
	return out
}

// FilterTraceIDsKeepSpans rebuilds td keeping only spans whose IDs are in keep.
func FilterTraceIDsKeepSpans(td ptrace.Traces, keep map[pcommon.SpanID]struct{}) ptrace.Traces {
	return CopyTraceKeepingSpans(td, keep)
}
