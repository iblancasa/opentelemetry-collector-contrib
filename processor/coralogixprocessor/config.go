// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package coralogixprocessor // import "github.com/open-telemetry/opentelemetry-collector-contrib/processor/coralogixprocessor"

type TransactionsConfig struct {
	Enabled bool `mapstructure:"enabled"`
}

type Config struct {
	TransactionsConfig `mapstructure:"transactions"`
}
