// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package coralogixprocessor // import "github.com/open-telemetry/opentelemetry-collector-contrib/processor/coralogixprocessor"

import (
	"context"
	"sync"
	"time"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/ptrace"
	"go.opentelemetry.io/collector/processor"
	"go.opentelemetry.io/collector/processor/processorhelper"
	"go.uber.org/zap"

	"github.com/open-telemetry/opentelemetry-collector-contrib/processor/coralogixprocessor/internal/criticalpath"
	"github.com/open-telemetry/opentelemetry-collector-contrib/processor/coralogixprocessor/internal/traceutil"
	"github.com/open-telemetry/opentelemetry-collector-contrib/processor/coralogixprocessor/internal/transactions"
)

type coralogixProcessor struct {
	config       *Config
	nextConsumer consumer.Traces
	logger       *zap.Logger

	harvestMu      sync.Mutex
	harvest        *transactions.PartitionedRegularTraceHeap
	harvestStop    chan struct{}
	harvestDone    chan struct{}
	harvestStarted bool
}

func newCoralogixProcessor(ctx context.Context, set processor.Settings, cfg *Config, nextConsumer consumer.Traces) (processor.Traces, error) {
	sp := &coralogixProcessor{
		config:       cfg,
		nextConsumer: nextConsumer,
		logger:       set.Logger.With(zap.String("component", "coralogixprocessor")),
	}

	if cfg.TransactionsConfig.Enabled && cfg.TransactionsConfig.MaxRegularTraces > 0 {
		sp.harvest = transactions.NewPartitionedRegularTraceHeap(cfg.TransactionsConfig.MaxRegularTraces)
		sp.harvestStop = make(chan struct{})
		sp.harvestDone = make(chan struct{})
	}

	return processorhelper.NewTraces(ctx,
		set,
		cfg,
		nextConsumer,
		sp.processTraces,
		processorhelper.WithCapabilities(consumer.Capabilities{MutatesData: true}),
		processorhelper.WithStart(sp.start),
		processorhelper.WithShutdown(sp.shutdown),
	)
}

func (sp *coralogixProcessor) start(ctx context.Context, _ component.Host) error {
	if sp.harvest == nil {
		return nil
	}
	go sp.harvestLoop(sp.config.TransactionsConfig.HarvestPeriod)
	sp.harvestStarted = true
	return nil
}

func (sp *coralogixProcessor) shutdown(ctx context.Context) error {
	if sp.harvest == nil {
		return nil
	}
	if !sp.harvestStarted {
		// Create+Shutdown without Start (lifecycle tests); nothing to wait for.
		return nil
	}
	close(sp.harvestStop)
	select {
	case <-sp.harvestDone:
	case <-ctx.Done():
	}
	sp.flushHarvest(context.Background())
	return nil
}

func (sp *coralogixProcessor) harvestLoop(period time.Duration) {
	defer close(sp.harvestDone)
	ticker := time.NewTicker(period)
	defer ticker.Stop()
	for {
		select {
		case <-sp.harvestStop:
			return
		case <-ticker.C:
			sp.flushHarvest(context.Background())
		}
	}
}

func (sp *coralogixProcessor) flushHarvest(ctx context.Context) {
	if sp.harvest == nil {
		return
	}
	sp.harvestMu.Lock()
	winners := sp.harvest.Drain()
	sp.harvestMu.Unlock()

	for _, winner := range winners {
		if winner.Traces.SpanCount() == 0 {
			continue
		}
		if err := sp.nextConsumer.ConsumeTraces(ctx, winner.Traces); err != nil {
			sp.logger.Error("failed to forward harvested transaction trace", zap.Error(err))
		}
	}
}

func (sp *coralogixProcessor) processTraces(_ context.Context, td ptrace.Traces) (ptrace.Traces, error) {
	if !sp.config.TransactionsConfig.Enabled && !sp.config.CriticalPathConfig.Enabled {
		return td, nil
	}
	if td.SpanCount() == 0 {
		return td, nil
	}

	spansByTraceID := traceutil.GroupSpansByTraceID(td)
	opts := transactions.ProcessOptions{MaxNodes: sp.config.TransactionsConfig.MaxNodes}
	harvesting := sp.config.TransactionsConfig.Enabled && sp.config.TransactionsConfig.MaxRegularTraces > 0

	if sp.config.TransactionsConfig.Enabled && sp.config.CriticalPathConfig.Enabled {
		transactionLogger := sp.logger.With(zap.String("feature", "transactions"))
		criticalPathLogger := sp.logger.With(zap.String("feature", "critical_path"))
		keepAll := make(map[pcommon.SpanID]struct{})
		for traceID, spans := range spansByTraceID {
			tree := traceutil.BuildTraceTree(spans)
			processed := transactions.ApplyTransactionAttributesToTreeWithOptions(tree, transactionLogger, opts)
			criticalpath.ApplyCriticalPathAttributesToTree(traceID, tree, criticalPathLogger)
			if harvesting {
				sp.witnessProcessed(td, processed)
			} else {
				for id := range processed.KeptIDs {
					keepAll[id] = struct{}{}
				}
			}
		}
		if harvesting {
			return ptrace.NewTraces(), nil
		}
		return transactions.FilterTraceIDsKeepSpans(td, keepAll), nil
	}

	if sp.config.TransactionsConfig.Enabled {
		transactionLogger := sp.logger.With(zap.String("feature", "transactions"))
		keepAll := make(map[pcommon.SpanID]struct{})
		for _, spans := range spansByTraceID {
			processed := transactions.ProcessCompletedTrace(spans, transactionLogger, opts)
			if harvesting {
				sp.witnessProcessed(td, processed)
			} else {
				for id := range processed.KeptIDs {
					keepAll[id] = struct{}{}
				}
			}
		}
		if harvesting {
			return ptrace.NewTraces(), nil
		}
		if sp.config.TransactionsConfig.MaxNodes > 0 {
			return transactions.FilterTraceIDsKeepSpans(td, keepAll), nil
		}
		return td, nil
	}

	if sp.config.CriticalPathConfig.Enabled {
		criticalpath.ApplyCriticalPathAttributesByTraceID(
			spansByTraceID,
			sp.logger.With(zap.String("feature", "critical_path")),
		)
	}

	return td, nil
}

func (sp *coralogixProcessor) witnessProcessed(td ptrace.Traces, processed transactions.ProcessedTrace) {
	if len(processed.KeptIDs) == 0 {
		return
	}
	partition := transactions.HarvestPartitionKey(td, processed.RootSpanID, processed.KeptIDs)
	candidate := transactions.HarvestTrace{
		DurationNS: processed.DurationNS,
		Partition:  partition,
		Traces:     transactions.CopyTraceKeepingSpans(td, processed.KeptIDs),
	}
	sp.harvestMu.Lock()
	sp.harvest.Witness(candidate)
	sp.harvestMu.Unlock()
}
