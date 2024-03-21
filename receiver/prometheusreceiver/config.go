// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package prometheusreceiver // import "github.com/open-telemetry/opentelemetry-collector-contrib/receiver/prometheusreceiver"

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"

	commonconfig "github.com/prometheus/common/config"
	promconfig "github.com/prometheus/prometheus/config"
	promHTTP "github.com/prometheus/prometheus/discovery/http"
	"github.com/prometheus/prometheus/discovery/kubernetes"
	"go.opentelemetry.io/collector/config/confighttp"
	"go.opentelemetry.io/collector/confmap"
	"gopkg.in/yaml.v2"
)

// Config defines configuration for Prometheus receiver.
type Config struct {
	PrometheusConfig   *PromConfig `mapstructure:"config"`
	TrimMetricSuffixes bool        `mapstructure:"trim_metric_suffixes"`
	// UseStartTimeMetric enables retrieving the start time of all counter metrics
	// from the process_start_time_seconds metric. This is only correct if all counters on that endpoint
	// started after the process start time, and the process is the only actor exporting the metric after
	// the process started. It should not be used in "exporters" which export counters that may have
	// started before the process itself. Use only if you know what you are doing, as this may result
	// in incorrect rate calculations.
	UseStartTimeMetric   bool   `mapstructure:"use_start_time_metric"`
	StartTimeMetricRegex string `mapstructure:"start_time_metric_regex"`

	// ReportExtraScrapeMetrics - enables reporting of additional metrics for Prometheus client like scrape_body_size_bytes
	ReportExtraScrapeMetrics bool `mapstructure:"report_extra_scrape_metrics"`

	TargetAllocator *TargetAllocator `mapstructure:"target_allocator"`

	// EnableProtobufNegotiation allows the collector to set the scraper option for
	// protobuf negotiation when conferring with a prometheus client.
	EnableProtobufNegotiation bool `mapstructure:"enable_protobuf_negotiation"`
}

// Validate checks the receiver configuration is valid.
func (cfg *Config) Validate() error {
	if (cfg.PrometheusConfig == nil || len(cfg.PrometheusConfig.ScrapeConfigs) == 0) && cfg.TargetAllocator == nil {
		return errors.New("no Prometheus scrape_configs or target_allocator set")
	}
	return nil
}

type TargetAllocator struct {
	confighttp.ClientConfig `mapstructure:",squash"`
	Interval                time.Duration     `mapstructure:"interval"`
	CollectorID             string            `mapstructure:"collector_id"`
	HTTPSDConfig            *PromHTTPSDConfig `mapstructure:"http_sd_config"`
}

func (cfg *TargetAllocator) Validate() error {
	// ensure valid endpoint
	if _, err := url.ParseRequestURI(cfg.Endpoint); err != nil {
		return fmt.Errorf("TargetAllocator endpoint is not valid: %s", cfg.Endpoint)
	}
	// ensure valid collectorID without variables
	if cfg.CollectorID == "" || strings.Contains(cfg.CollectorID, "${") {
		return fmt.Errorf("CollectorID is not a valid ID")
	}

	return nil
}

// PromConfig is a redeclaration of promconfig.Config because we need custom unmarshaling
// as prometheus "config" uses `yaml` tags.
type PromConfig promconfig.Config

var _ confmap.Unmarshaler = (*PromConfig)(nil)

func (cfg *PromConfig) Unmarshal(componentParser *confmap.Conf) error {
	cfgMap := componentParser.ToStringMap()
	if len(cfgMap) == 0 {
		return nil
	}
	return unmarshalYAML(cfgMap, (*promconfig.Config)(cfg))
}

func (cfg *PromConfig) Validate() error {
	// Reject features that Prometheus supports but that the receiver doesn't support:
	// See:
	// * https://github.com/open-telemetry/opentelemetry-collector/issues/3863
	// * https://github.com/open-telemetry/wg-prometheus/issues/3
	unsupportedFeatures := make([]string, 0, 4)
	if len(cfg.RemoteWriteConfigs) != 0 {
		unsupportedFeatures = append(unsupportedFeatures, "remote_write")
	}
	if len(cfg.RemoteReadConfigs) != 0 {
		unsupportedFeatures = append(unsupportedFeatures, "remote_read")
	}
	if len(cfg.RuleFiles) != 0 {
		unsupportedFeatures = append(unsupportedFeatures, "rule_files")
	}
	if len(cfg.AlertingConfig.AlertRelabelConfigs) != 0 {
		unsupportedFeatures = append(unsupportedFeatures, "alert_config.relabel_configs")
	}
	if len(cfg.AlertingConfig.AlertmanagerConfigs) != 0 {
		unsupportedFeatures = append(unsupportedFeatures, "alert_config.alertmanagers")
	}
	if len(unsupportedFeatures) != 0 {
		// Sort the values for deterministic error messages.
		sort.Strings(unsupportedFeatures)
		return fmt.Errorf("unsupported features:\n\t%s", strings.Join(unsupportedFeatures, "\n\t"))
	}

	for _, sc := range cfg.ScrapeConfigs {
		if sc.HTTPClientConfig.Authorization != nil {
			if err := checkFile(sc.HTTPClientConfig.Authorization.CredentialsFile); err != nil {
				return fmt.Errorf("error checking authorization credentials file %q: %w", sc.HTTPClientConfig.Authorization.CredentialsFile, err)
			}
		}

		if err := checkTLSConfig(sc.HTTPClientConfig.TLSConfig); err != nil {
			return err
		}

		for _, c := range sc.ServiceDiscoveryConfigs {
			if c, ok := c.(*kubernetes.SDConfig); ok {
				if err := checkTLSConfig(c.HTTPClientConfig.TLSConfig); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// PromHTTPSDConfig is a redeclaration of promHTTP.SDConfig because we need custom unmarshaling
// as prometheus "config" uses `yaml` tags.
type PromHTTPSDConfig promHTTP.SDConfig

var _ confmap.Unmarshaler = (*PromHTTPSDConfig)(nil)

func (cfg *PromHTTPSDConfig) Unmarshal(componentParser *confmap.Conf) error {
	cfgMap := componentParser.ToStringMap()
	if len(cfgMap) == 0 {
		return nil
	}
	cfgMap["url"] = "http://placeholder" // we have to set it as else marshaling will fail
	return unmarshalYAML(cfgMap, (*promHTTP.SDConfig)(cfg))
}

func unmarshalYAML(in map[string]any, out any) error {
	yamlOut, err := yaml.Marshal(in)
	if err != nil {
		return fmt.Errorf("prometheus receiver: failed to marshal config to yaml: %w", err)
	}

	err = yaml.UnmarshalStrict(yamlOut, out)
	if err != nil {
		return fmt.Errorf("prometheus receiver: failed to unmarshal yaml to prometheus config object: %w", err)
	}
	return nil
}

func checkFile(fn string) error {
	// Nothing set, nothing to error on.
	if fn == "" {
		return nil
	}
	_, err := os.Stat(fn)
	return err
}

func checkTLSConfig(tlsConfig commonconfig.TLSConfig) error {
	if err := checkFile(tlsConfig.CertFile); err != nil {
		return fmt.Errorf("error checking client cert file %q: %w", tlsConfig.CertFile, err)
	}
	if err := checkFile(tlsConfig.KeyFile); err != nil {
		return fmt.Errorf("error checking client key file %q: %w", tlsConfig.KeyFile, err)
	}
	return nil
}
