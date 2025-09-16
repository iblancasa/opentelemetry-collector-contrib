// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package url // import "github.com/open-telemetry/opentelemetry-collector-contrib/processor/redactionprocessor/internal/url"

import (
	"fmt"

	"github.com/grafana/clusterurl/pkg/clusterurl"
	"go.uber.org/zap"
)

type URLSanitizer struct {
	classifier *clusterurl.ClusterURLClassifier
	attributes map[string]bool
	logger     *zap.Logger
}

func NewURLSanitizer(config URLSanitizationConfig, logger *zap.Logger) (*URLSanitizer, error) {
	classifier, err := clusterurl.NewClusterURLClassifier(nil)
	if err != nil {
		return nil, fmt.Errorf("unable to create cluster URL classifier: %w", err)
	}

	attributes := make(map[string]bool)
	for _, attribute := range config.Attributes {
		attributes[attribute] = true
	}

	return &URLSanitizer{
		classifier: classifier,
		attributes: attributes,
		logger:     logger.Named("url_sanitizer"),
	}, nil
}

func (s *URLSanitizer) SanitizeAttributeURL(url, attributeKey string) string {
	if url == "" {
		return url
	}

	if _, ok := s.attributes[attributeKey]; ok {
		s.logger.Debug("Sanitizing attribute URL",
			zap.String("attribute_key", attributeKey),
			zap.String("original_url", url))
		sanitized := s.SanitizeURL(url)
		if sanitized != url {
			s.logger.Debug("URL was sanitized for attribute",
				zap.String("attribute_key", attributeKey),
				zap.String("original_url", url),
				zap.String("sanitized_url", sanitized))
		}
		return sanitized
	}

	s.logger.Debug("Skipping URL sanitization - attribute not configured",
		zap.String("attribute_key", attributeKey),
		zap.String("url", url),
		zap.Strings("configured_attributes", s.getConfiguredAttributes()))

	return url
}

// SanitizeURL sanitizes the given URL by removing any gibberish words.
// https://github.com/open-telemetry/opentelemetry-ebpf-instrumentation/blob/38ca7938595409b8ffe6b897c14a0e3280dd2941/pkg/components/transform/route/cluster.go#L48
func (s *URLSanitizer) SanitizeURL(url string) string {
	s.logger.Debug("Sanitizing URL",
		zap.String("original_url", url))
	sanitized := s.classifier.ClusterURL(url)
	if sanitized != url {
		s.logger.Debug("URL was sanitized",
			zap.String("original_url", url),
			zap.String("sanitized_url", sanitized))
	}
	return sanitized
}

func (s *URLSanitizer) getConfiguredAttributes() []string {
	attrs := make([]string, 0, len(s.attributes))
	for attr := range s.attributes {
		attrs = append(attrs, attr)
	}
	return attrs
}
