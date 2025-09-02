// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package url // import "github.com/open-telemetry/opentelemetry-collector-contrib/processor/redactionprocessor/internal/url"

type URLSanitizationConfig struct {
	Enabled bool `mapstructure:"enabled"`
	// Attributes is the list of attributes that will be sanitized.
	Attributes []string `mapstructure:"attributes"`

	// SanitizeAllAttributes specifies whether to sanitize all attributes.
	// If true, URL sanitization is applied to all string values in telemetry attributes.
	// Log bodies will be sanitized as well.
	SanitizeAllAttributes bool `mapstructure:"sanitize_all_attributes"`
}
