// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package transactions

import (
	"errors"
	"strings"

	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"go.uber.org/zap"
)

const (
	TransactionIdentifier           = "cgx.transaction"
	TransactionIdentifierRoot       = "cgx.transaction.root"
	TransactionIdentifierTraceState = "cgx_transaction"
)

func ApplyTransactionsAttributes(td ptrace.Traces, logger *zap.Logger) (ptrace.Traces, error) {
	spanCount := td.SpanCount()

	if spanCount == 0 {
		logger.Info("no spans found in the trace")
		return td, nil
	}

	rs := td.ResourceSpans()
	for i := 0; i < rs.Len(); i++ {
		resourceSpan := rs.At(i)

		spanIDSet := make(map[pcommon.SpanID]bool)
		spanList := make([]ptrace.Span, 0)

		scopeSpans := resourceSpan.ScopeSpans()
		for j := 0; j < scopeSpans.Len(); j++ {
			spans := scopeSpans.At(j).Spans()
			for k := 0; k < spans.Len(); k++ {
				span := spans.At(k)
				spanIDSet[span.SpanID()] = true
				spanList = append(spanList, span)
			}
		}

		rootSpan, err := findRootSpan(spanList, spanIDSet)
		if err != nil {
			logger.Error("error while looking for root span", zap.Error(err))
			continue
		}
		rootSpan.Attributes().PutBool(TransactionIdentifierRoot, true)
		spanName := rootSpan.Name()
		applyTransactionAttributes(spanName, spanList)
	}
	return td, nil
}

func findRootSpan(spanList []ptrace.Span, spanIDSet map[pcommon.SpanID]bool) (ptrace.Span, error) {
	for _, span := range spanList {
		parentID := span.ParentSpanID()
		if parentID.IsEmpty() || !spanIDSet[parentID] {
			return span, nil
		}
	}
	return ptrace.Span{}, errors.New("no root span found in the span list")
}

func applyTransactionAttributes(rootServiceSpanName string, spanList []ptrace.Span) {
	for _, span := range spanList {
		span.Attributes().PutStr(TransactionIdentifier, rootServiceSpanName)
		if shouldUpdateTraceState(span) {
			updateTraceState(span, rootServiceSpanName)
		}
	}
}

func updateTraceState(span ptrace.Span, transactionName string) {
	currentTraceState := span.TraceState().AsRaw()
	newTraceState := updateTraceStateKV(currentTraceState, TransactionIdentifierTraceState, transactionName)
	span.TraceState().FromRaw(newTraceState)
}

func updateTraceStateKV(traceState, key, value string) string {
	pairs := []string{}
	found := false

	for _, pair := range strings.Split(traceState, ",") {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		kv := strings.SplitN(pair, "=", 2)
		if len(kv) != 2 {
			continue
		}
		if kv[0] == key {
			pairs = append(pairs, key+"="+value)
			found = true
		} else {
			pairs = append(pairs, pair)
		}
	}
	if !found {
		pairs = append(pairs, key+"="+value)
	}
	return strings.Join(pairs, ",")
}

func shouldUpdateTraceState(span ptrace.Span) bool {
	kind := span.Kind()
	if kind == ptrace.SpanKindServer || kind == ptrace.SpanKindConsumer {
		return false
	}

	currentTraceState := span.TraceState().AsRaw()
	return !strings.Contains(currentTraceState, TransactionIdentifierTraceState+"=")
}
