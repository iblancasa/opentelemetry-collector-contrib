// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package transactions

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"go.uber.org/zap"
)

func TestApplyTransactionsAttributes_EmptyTraces(t *testing.T) {
	logger := zap.NewNop()
	td := ptrace.NewTraces()

	result, err := ApplyTransactionsAttributes(td, logger)

	assert.NoError(t, err)
	assert.Equal(t, td, result)
}

func TestApplyTransactionsAttributes_SingleSpan(t *testing.T) {
	logger := zap.NewNop()
	td := ptrace.NewTraces()

	// Create a single span
	rs := td.ResourceSpans().AppendEmpty()
	scopeSpans := rs.ScopeSpans().AppendEmpty()
	span := scopeSpans.Spans().AppendEmpty()
	span.SetName("test-span")
	span.SetSpanID(pcommon.SpanID([8]byte{1, 2, 3, 4, 5, 6, 7, 8}))
	span.SetTraceID(pcommon.TraceID([16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}))
	// No parent span ID - this is the root span

	result, err := ApplyTransactionsAttributes(td, logger)

	assert.NoError(t, err)
	assert.Equal(t, 1, result.SpanCount())

	// Check that the span has the expected attributes
	resultSpan := result.ResourceSpans().At(0).ScopeSpans().At(0).Spans().At(0)
	assert.Equal(t, "test-span", resultSpan.Attributes().AsRaw()[TransactionIdentifier])
	assert.Equal(t, "cgx_transaction=test-span", resultSpan.TraceState().AsRaw())
}

func TestApplyTransactionsAttributes_ParentChildSpans(t *testing.T) {
	logger := zap.NewNop()
	td := ptrace.NewTraces()

	// Create parent and child spans
	rs := td.ResourceSpans().AppendEmpty()
	scopeSpans := rs.ScopeSpans().AppendEmpty()

	// Parent span (root)
	parentSpan := scopeSpans.Spans().AppendEmpty()
	parentSpan.SetName("parent-span")
	parentSpanID := pcommon.SpanID([8]byte{1, 2, 3, 4, 5, 6, 7, 8})
	parentSpan.SetSpanID(parentSpanID)
	parentSpan.SetTraceID(pcommon.TraceID([16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}))

	// Child span
	childSpan := scopeSpans.Spans().AppendEmpty()
	childSpan.SetName("child-span")
	childSpan.SetSpanID(pcommon.SpanID([8]byte{9, 10, 11, 12, 13, 14, 15, 16}))
	childSpan.SetTraceID(pcommon.TraceID([16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}))
	childSpan.SetParentSpanID(parentSpanID)

	result, err := ApplyTransactionsAttributes(td, logger)

	assert.NoError(t, err)
	assert.Equal(t, 2, result.SpanCount())

	// Check parent span attributes
	resultParentSpan := result.ResourceSpans().At(0).ScopeSpans().At(0).Spans().At(0)
	assert.Equal(t, "parent-span", resultParentSpan.Attributes().AsRaw()[TransactionIdentifier])
	assert.Equal(t, "cgx_transaction=parent-span", resultParentSpan.TraceState().AsRaw())

	// Check child span attributes
	resultChildSpan := result.ResourceSpans().At(0).ScopeSpans().At(0).Spans().At(1)
	assert.Equal(t, "parent-span", resultChildSpan.Attributes().AsRaw()[TransactionIdentifier])
	assert.Equal(t, "cgx_transaction=parent-span", resultChildSpan.TraceState().AsRaw())
}

func TestApplyTransactionsAttributes_MultipleResourceSpans(t *testing.T) {
	logger := zap.NewNop()
	td := ptrace.NewTraces()

	// First resource spans
	rs1 := td.ResourceSpans().AppendEmpty()
	scopeSpans1 := rs1.ScopeSpans().AppendEmpty()
	span1 := scopeSpans1.Spans().AppendEmpty()
	span1.SetName("span-1")
	span1.SetSpanID(pcommon.SpanID([8]byte{1, 2, 3, 4, 5, 6, 7, 8}))
	span1.SetTraceID(pcommon.TraceID([16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}))

	// Second resource spans
	rs2 := td.ResourceSpans().AppendEmpty()
	scopeSpans2 := rs2.ScopeSpans().AppendEmpty()
	span2 := scopeSpans2.Spans().AppendEmpty()
	span2.SetName("span-2")
	span2.SetSpanID(pcommon.SpanID([8]byte{9, 10, 11, 12, 13, 14, 15, 16}))
	span2.SetTraceID(pcommon.TraceID([16]byte{17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32}))

	result, err := ApplyTransactionsAttributes(td, logger)

	assert.NoError(t, err)
	assert.Equal(t, 2, result.SpanCount())

	// Check first span
	resultSpan1 := result.ResourceSpans().At(0).ScopeSpans().At(0).Spans().At(0)
	assert.Equal(t, "span-1", resultSpan1.Attributes().AsRaw()[TransactionIdentifier])

	// Check second span
	resultSpan2 := result.ResourceSpans().At(1).ScopeSpans().At(0).Spans().At(0)
	assert.Equal(t, "span-2", resultSpan2.Attributes().AsRaw()[TransactionIdentifier])
}

func TestApplyTransactionsAttributes_NoRootSpan(t *testing.T) {
	logger := zap.NewNop()
	td := ptrace.NewTraces()

	// Create spans where all have parents that don't exist in the set
	rs := td.ResourceSpans().AppendEmpty()
	scopeSpans := rs.ScopeSpans().AppendEmpty()

	span1 := scopeSpans.Spans().AppendEmpty()
	span1.SetName("span-1")
	span1.SetSpanID(pcommon.SpanID([8]byte{1, 2, 3, 4, 5, 6, 7, 8}))
	span1.SetTraceID(pcommon.TraceID([16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}))
	span1.SetParentSpanID(pcommon.SpanID([8]byte{99, 99, 99, 99, 99, 99, 99, 99})) // Non-existent parent

	span2 := scopeSpans.Spans().AppendEmpty()
	span2.SetName("span-2")
	span2.SetSpanID(pcommon.SpanID([8]byte{9, 10, 11, 12, 13, 14, 15, 16}))
	span2.SetTraceID(pcommon.TraceID([16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}))
	span2.SetParentSpanID(pcommon.SpanID([8]byte{88, 88, 88, 88, 88, 88, 88, 88})) // Non-existent parent

	result, err := ApplyTransactionsAttributes(td, logger)

	// Should not error, but should log the error and continue
	assert.NoError(t, err)
	assert.Equal(t, 2, result.SpanCount())
}

func TestFindRootSpan_SingleSpan(t *testing.T) {
	td := ptrace.NewTraces()
	rs := td.ResourceSpans().AppendEmpty()
	scopeSpans := rs.ScopeSpans().AppendEmpty()
	span := scopeSpans.Spans().AppendEmpty()
	span.SetSpanID(pcommon.SpanID([8]byte{1, 2, 3, 4, 5, 6, 7, 8}))

	spanList := []ptrace.Span{span}
	spanIDSet := map[pcommon.SpanID]bool{
		span.SpanID(): true,
	}

	rootSpan, err := findRootSpan(spanList, spanIDSet)

	assert.NoError(t, err)
	assert.Equal(t, span.SpanID(), rootSpan.SpanID())
}

func TestFindRootSpan_ParentChild(t *testing.T) {
	td := ptrace.NewTraces()
	rs := td.ResourceSpans().AppendEmpty()
	scopeSpans := rs.ScopeSpans().AppendEmpty()

	parentSpan := scopeSpans.Spans().AppendEmpty()
	parentSpanID := pcommon.SpanID([8]byte{1, 2, 3, 4, 5, 6, 7, 8})
	parentSpan.SetSpanID(parentSpanID)

	childSpan := scopeSpans.Spans().AppendEmpty()
	childSpan.SetSpanID(pcommon.SpanID([8]byte{9, 10, 11, 12, 13, 14, 15, 16}))
	childSpan.SetParentSpanID(parentSpanID)

	spanList := []ptrace.Span{parentSpan, childSpan}
	spanIDSet := map[pcommon.SpanID]bool{
		parentSpan.SpanID(): true,
		childSpan.SpanID():  true,
	}

	rootSpan, err := findRootSpan(spanList, spanIDSet)

	assert.NoError(t, err)
	assert.Equal(t, parentSpanID, rootSpan.SpanID())
}

func TestFindRootSpan_NoRootSpan(t *testing.T) {
	td := ptrace.NewTraces()
	rs := td.ResourceSpans().AppendEmpty()
	scopeSpans := rs.ScopeSpans().AppendEmpty()

	// Create a cycle: span1's parent is span2, span2's parent is span1
	span1 := scopeSpans.Spans().AppendEmpty()
	span1.SetSpanID(pcommon.SpanID([8]byte{1, 2, 3, 4, 5, 6, 7, 8}))
	span2 := scopeSpans.Spans().AppendEmpty()
	span2.SetSpanID(pcommon.SpanID([8]byte{9, 10, 11, 12, 13, 14, 15, 16}))
	span1.SetParentSpanID(span2.SpanID())
	span2.SetParentSpanID(span1.SpanID())

	spanList := []ptrace.Span{span1, span2}
	spanIDSet := map[pcommon.SpanID]bool{
		span1.SpanID(): true,
		span2.SpanID(): true,
	}

	rootSpan, err := findRootSpan(spanList, spanIDSet)
	if err == nil {
		t.Errorf("An error is expected but got nil. rootSpan: %+v", rootSpan)
		return
	}
	assert.Equal(t, "no root span found in the span list", err.Error())
}

func TestApplyTransactionAttributes(t *testing.T) {
	td := ptrace.NewTraces()
	rs := td.ResourceSpans().AppendEmpty()
	scopeSpans := rs.ScopeSpans().AppendEmpty()

	span1 := scopeSpans.Spans().AppendEmpty()
	span1.SetName("span-1")
	span1.SetSpanID(pcommon.SpanID([8]byte{1, 2, 3, 4, 5, 6, 7, 8}))

	span2 := scopeSpans.Spans().AppendEmpty()
	span2.SetName("span-2")
	span2.SetSpanID(pcommon.SpanID([8]byte{9, 10, 11, 12, 13, 14, 15, 16}))

	spanList := []ptrace.Span{span1, span2}
	rootServiceSpanName := "root-service"

	applyTransactionAttributes(rootServiceSpanName, spanList)

	// Check that both spans have the correct attributes
	assert.Equal(t, rootServiceSpanName, span1.Attributes().AsRaw()[TransactionIdentifier])
	assert.Equal(t, TransactionIdentifierTraceState+"="+rootServiceSpanName, span1.TraceState().AsRaw())

	assert.Equal(t, rootServiceSpanName, span2.Attributes().AsRaw()[TransactionIdentifier])
	assert.Equal(t, TransactionIdentifierTraceState+"="+rootServiceSpanName, span2.TraceState().AsRaw())
}

func TestUpdateTraceStateKV_EmptyTraceState(t *testing.T) {
	result := updateTraceStateKV("", "test-key", "test-value")
	assert.Equal(t, "test-key=test-value", result)
}

func TestUpdateTraceStateKV_AddNewKey(t *testing.T) {
	result := updateTraceStateKV("existing=value", "test-key", "test-value")
	assert.Equal(t, "existing=value,test-key=test-value", result)
}

func TestUpdateTraceStateKV_UpdateExistingKey(t *testing.T) {
	result := updateTraceStateKV("existing=old,test-key=old-value,other=value", "test-key", "new-value")
	assert.Equal(t, "existing=old,test-key=new-value,other=value", result)
}

func TestUpdateTraceStateKV_MalformedPairs(t *testing.T) {
	result := updateTraceStateKV("malformed,key=value,another-malformed", "key", "new-value")
	assert.Equal(t, "key=new-value", result)
}

func TestUpdateTraceStateKV_EmptyPairs(t *testing.T) {
	result := updateTraceStateKV("  ,  ,key=value,  ", "key", "new-value")
	assert.Equal(t, "key=new-value", result)
}

func TestUpdateTraceStateKV_KeyOnlyPair(t *testing.T) {
	result := updateTraceStateKV("key-only,valid=value", "valid", "new-value")
	assert.Equal(t, "valid=new-value", result)
}

func TestShouldUpdateTraceState_ServerSpan(t *testing.T) {
	td := ptrace.NewTraces()
	rs := td.ResourceSpans().AppendEmpty()
	scopeSpans := rs.ScopeSpans().AppendEmpty()
	span := scopeSpans.Spans().AppendEmpty()
	span.SetKind(ptrace.SpanKindServer)

	result := shouldUpdateTraceState(span)
	assert.False(t, result)
}

func TestShouldUpdateTraceState_ConsumerSpan(t *testing.T) {
	td := ptrace.NewTraces()
	rs := td.ResourceSpans().AppendEmpty()
	scopeSpans := rs.ScopeSpans().AppendEmpty()
	span := scopeSpans.Spans().AppendEmpty()
	span.SetKind(ptrace.SpanKindConsumer)

	result := shouldUpdateTraceState(span)
	assert.False(t, result)
}

func TestShouldUpdateTraceState_AlreadyHasTransaction(t *testing.T) {
	td := ptrace.NewTraces()
	rs := td.ResourceSpans().AppendEmpty()
	scopeSpans := rs.ScopeSpans().AppendEmpty()
	span := scopeSpans.Spans().AppendEmpty()
	span.SetKind(ptrace.SpanKindClient)
	span.TraceState().FromRaw("cgx_transaction=existing-transaction")

	result := shouldUpdateTraceState(span)
	assert.False(t, result)
}

func TestShouldUpdateTraceState_ShouldUpdate(t *testing.T) {
	td := ptrace.NewTraces()
	rs := td.ResourceSpans().AppendEmpty()
	scopeSpans := rs.ScopeSpans().AppendEmpty()
	span := scopeSpans.Spans().AppendEmpty()
	span.SetKind(ptrace.SpanKindClient)
	span.TraceState().FromRaw("other=value")

	result := shouldUpdateTraceState(span)
	assert.True(t, result)
}

func TestUpdateTraceState_ClientSpan(t *testing.T) {
	td := ptrace.NewTraces()
	rs := td.ResourceSpans().AppendEmpty()
	scopeSpans := rs.ScopeSpans().AppendEmpty()
	span := scopeSpans.Spans().AppendEmpty()
	span.SetKind(ptrace.SpanKindClient)
	span.TraceState().FromRaw("existing=value")

	updateTraceState(span, "test-transaction")

	expected := "existing=value,cgx_transaction=test-transaction"
	assert.Equal(t, expected, span.TraceState().AsRaw())
}

func TestUpdateTraceState_EmptyTraceState(t *testing.T) {
	td := ptrace.NewTraces()
	rs := td.ResourceSpans().AppendEmpty()
	scopeSpans := rs.ScopeSpans().AppendEmpty()
	span := scopeSpans.Spans().AppendEmpty()
	span.SetKind(ptrace.SpanKindClient)

	updateTraceState(span, "test-transaction")

	expected := "cgx_transaction=test-transaction"
	assert.Equal(t, expected, span.TraceState().AsRaw())
}

func TestApplyTransactionsAttributes_ServerSpanNoTraceStateUpdate(t *testing.T) {
	logger := zap.NewNop()
	td := ptrace.NewTraces()

	rs := td.ResourceSpans().AppendEmpty()
	scopeSpans := rs.ScopeSpans().AppendEmpty()
	span := scopeSpans.Spans().AppendEmpty()
	span.SetName("server-span")
	span.SetSpanID(pcommon.SpanID([8]byte{1, 2, 3, 4, 5, 6, 7, 8}))
	span.SetTraceID(pcommon.TraceID([16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}))
	span.SetKind(ptrace.SpanKindServer)
	span.TraceState().FromRaw("original=state")

	result, err := ApplyTransactionsAttributes(td, logger)

	assert.NoError(t, err)
	assert.Equal(t, 1, result.SpanCount())

	resultSpan := result.ResourceSpans().At(0).ScopeSpans().At(0).Spans().At(0)
	assert.Equal(t, "server-span", resultSpan.Attributes().AsRaw()[TransactionIdentifier])
	// Server spans should not have trace state updated
	assert.Equal(t, "original=state", resultSpan.TraceState().AsRaw())
}

func TestApplyTransactionsAttributes_ConsumerSpanNoTraceStateUpdate(t *testing.T) {
	logger := zap.NewNop()
	td := ptrace.NewTraces()

	rs := td.ResourceSpans().AppendEmpty()
	scopeSpans := rs.ScopeSpans().AppendEmpty()
	span := scopeSpans.Spans().AppendEmpty()
	span.SetName("consumer-span")
	span.SetSpanID(pcommon.SpanID([8]byte{1, 2, 3, 4, 5, 6, 7, 8}))
	span.SetTraceID(pcommon.TraceID([16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}))
	span.SetKind(ptrace.SpanKindConsumer)
	span.TraceState().FromRaw("original=state")

	result, err := ApplyTransactionsAttributes(td, logger)

	assert.NoError(t, err)
	assert.Equal(t, 1, result.SpanCount())

	resultSpan := result.ResourceSpans().At(0).ScopeSpans().At(0).Spans().At(0)
	assert.Equal(t, "consumer-span", resultSpan.Attributes().AsRaw()[TransactionIdentifier])
	// Consumer spans should not have trace state updated
	assert.Equal(t, "original=state", resultSpan.TraceState().AsRaw())
}

func TestApplyTransactionsAttributes_SpanWithExistingTransaction(t *testing.T) {
	logger := zap.NewNop()
	td := ptrace.NewTraces()

	rs := td.ResourceSpans().AppendEmpty()
	scopeSpans := rs.ScopeSpans().AppendEmpty()
	span := scopeSpans.Spans().AppendEmpty()
	span.SetName("existing-transaction-span")
	span.SetSpanID(pcommon.SpanID([8]byte{1, 2, 3, 4, 5, 6, 7, 8}))
	span.SetTraceID(pcommon.TraceID([16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}))
	span.SetKind(ptrace.SpanKindClient)
	span.TraceState().FromRaw("cgx_transaction=existing-transaction,other=value")

	result, err := ApplyTransactionsAttributes(td, logger)

	assert.NoError(t, err)
	assert.Equal(t, 1, result.SpanCount())

	resultSpan := result.ResourceSpans().At(0).ScopeSpans().At(0).Spans().At(0)
	assert.Equal(t, "existing-transaction-span", resultSpan.Attributes().AsRaw()[TransactionIdentifier])
	// Should not update trace state if transaction already exists
	assert.Equal(t, "cgx_transaction=existing-transaction,other=value", resultSpan.TraceState().AsRaw())
}

func TestApplyTransactionsAttributes_MultipleScopeSpans(t *testing.T) {
	logger := zap.NewNop()
	td := ptrace.NewTraces()

	rs := td.ResourceSpans().AppendEmpty()

	// First scope spans
	scopeSpans1 := rs.ScopeSpans().AppendEmpty()
	span1 := scopeSpans1.Spans().AppendEmpty()
	span1.SetName("span-1")
	span1.SetSpanID(pcommon.SpanID([8]byte{1, 2, 3, 4, 5, 6, 7, 8}))
	span1.SetTraceID(pcommon.TraceID([16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}))

	// Second scope spans
	scopeSpans2 := rs.ScopeSpans().AppendEmpty()
	span2 := scopeSpans2.Spans().AppendEmpty()
	span2.SetName("span-2")
	span2.SetSpanID(pcommon.SpanID([8]byte{9, 10, 11, 12, 13, 14, 15, 16}))
	span2.SetTraceID(pcommon.TraceID([16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}))
	span2.SetParentSpanID(span1.SpanID())

	result, err := ApplyTransactionsAttributes(td, logger)

	assert.NoError(t, err)
	assert.Equal(t, 2, result.SpanCount())

	// Check first span (root)
	resultSpan1 := result.ResourceSpans().At(0).ScopeSpans().At(0).Spans().At(0)
	assert.Equal(t, "span-1", resultSpan1.Attributes().AsRaw()[TransactionIdentifier])
	assert.Equal(t, true, resultSpan1.Attributes().AsRaw()[TransactionIdentifierRoot])

	// Check second span (child)
	resultSpan2 := result.ResourceSpans().At(0).ScopeSpans().At(1).Spans().At(0)
	assert.Equal(t, "span-1", resultSpan2.Attributes().AsRaw()[TransactionIdentifier])
	assert.NotContains(t, resultSpan2.Attributes().AsRaw(), TransactionIdentifierRoot)
}

func TestApplyTransactionsAttributes_ComplexTraceState(t *testing.T) {
	logger := zap.NewNop()
	td := ptrace.NewTraces()

	rs := td.ResourceSpans().AppendEmpty()
	scopeSpans := rs.ScopeSpans().AppendEmpty()
	span := scopeSpans.Spans().AppendEmpty()
	span.SetName("complex-trace-state-span")
	span.SetSpanID(pcommon.SpanID([8]byte{1, 2, 3, 4, 5, 6, 7, 8}))
	span.SetTraceID(pcommon.TraceID([16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}))
	span.SetKind(ptrace.SpanKindClient)
	span.TraceState().FromRaw("sampled=1,other=value,third=key")

	result, err := ApplyTransactionsAttributes(td, logger)

	assert.NoError(t, err)
	assert.Equal(t, 1, result.SpanCount())

	resultSpan := result.ResourceSpans().At(0).ScopeSpans().At(0).Spans().At(0)
	assert.Equal(t, "complex-trace-state-span", resultSpan.Attributes().AsRaw()[TransactionIdentifier])
	// Should append transaction to existing trace state
	assert.Equal(t, "sampled=1,other=value,third=key,cgx_transaction=complex-trace-state-span", resultSpan.TraceState().AsRaw())
}

func TestApplyTransactionsAttributes_ErrorFindingRootSpan(t *testing.T) {
	logger := zap.NewNop()
	td := ptrace.NewTraces()

	rs := td.ResourceSpans().AppendEmpty()
	scopeSpans := rs.ScopeSpans().AppendEmpty()

	// Create a cycle: span1's parent is span2, span2's parent is span1
	span1 := scopeSpans.Spans().AppendEmpty()
	span1.SetName("span-1")
	span1.SetSpanID(pcommon.SpanID([8]byte{1, 2, 3, 4, 5, 6, 7, 8}))
	span2 := scopeSpans.Spans().AppendEmpty()
	span2.SetName("span-2")
	span2.SetSpanID(pcommon.SpanID([8]byte{9, 10, 11, 12, 13, 14, 15, 16}))
	span1.SetParentSpanID(span2.SpanID())
	span2.SetParentSpanID(span1.SpanID())

	result, err := ApplyTransactionsAttributes(td, logger)
	assert.NoError(t, err)
	assert.Equal(t, 2, result.SpanCount())
	// No span should have TransactionIdentifierRoot attribute
	for i := 0; i < result.ResourceSpans().At(0).ScopeSpans().At(0).Spans().Len(); i++ {
		attrs := result.ResourceSpans().At(0).ScopeSpans().At(0).Spans().At(i).Attributes().AsRaw()
		_, hasRoot := attrs[TransactionIdentifierRoot]
		assert.False(t, hasRoot)
	}
}
