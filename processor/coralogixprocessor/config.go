// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package coralogixprocessor // import "github.com/open-telemetry/opentelemetry-collector-contrib/processor/coralogixprocessor"

import (
	"errors"
	"fmt"
	"time"
)

const (
	defaultMaxTxnTraceNodes  = 256
	defaultMaxRegularTraces  = 1
	defaultHarvestPeriod     = 60 * time.Second
)

// TransactionsConfig holds configuration for transactions.
type TransactionsConfig struct {
	Enabled bool `mapstructure:"enabled"`

	// MaxNodes is the go-agent maxTxnTraceNodes limit: keep at most this many
	// spans per completed trace, preferring longer durations, always keeping
	// the transaction root. 0 disables node trimming.
	MaxNodes int `mapstructure:"max_nodes"`

	// MaxRegularTraces is the go-agent maxRegularTraces harvest limit per
	// partition: keep only the slowest completed traces per harvest window
	// for each (root service.name, transaction name, peer service set).
	// 0 disables harvest sampling and forwards every completed trace after
	// node trim.
	MaxRegularTraces int `mapstructure:"max_regular_traces"`

	// HarvestPeriod is the harvest window used when MaxRegularTraces > 0.
	HarvestPeriod time.Duration `mapstructure:"harvest_period"`

	_ struct{} // prevents unkeyed literal initialization
}

// CriticalPathConfig holds configuration for critical path processing.
type CriticalPathConfig struct {
	Enabled bool     `mapstructure:"enabled"`
	_       struct{} // prevents unkeyed literal initialization
}

type Config struct {
	TransactionsConfig `mapstructure:"transactions"`
	CriticalPathConfig `mapstructure:"critical_path"`
	// prevents unkeyed literal initialization
	_ struct{}
}

func (c *Config) Validate() error {
	if c.TransactionsConfig.MaxNodes < 0 {
		return errors.New("transactions.max_nodes must be >= 0")
	}
	if c.TransactionsConfig.MaxRegularTraces < 0 {
		return errors.New("transactions.max_regular_traces must be >= 0")
	}
	if c.TransactionsConfig.MaxRegularTraces > 0 && c.TransactionsConfig.HarvestPeriod <= 0 {
		return fmt.Errorf("transactions.harvest_period must be positive when max_regular_traces > 0, got %v", c.TransactionsConfig.HarvestPeriod)
	}
	return nil
}
