// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0
package url

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewURLSanitizer(t *testing.T) {
	tests := []struct {
		name   string
		config URLSanitizationConfig
		wantOK bool
	}{
		{
			name: "valid config with attributes",
			config: URLSanitizationConfig{
				Enabled:    true,
				Attributes: []string{"http.url", "url"},
			},
			wantOK: true,
		},
		{
			name: "valid config with sanitize all attributes",
			config: URLSanitizationConfig{
				Enabled:               true,
				SanitizeAllAttributes: true,
			},
			wantOK: true,
		},
		{
			name: "valid config with both attributes and sanitize all",
			config: URLSanitizationConfig{
				Enabled:               true,
				Attributes:            []string{"http.url"},
				SanitizeAllAttributes: true,
			},
			wantOK: true,
		},
		{
			name: "disabled config",
			config: URLSanitizationConfig{
				Enabled: false,
			},
			wantOK: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sanitizer, err := NewURLSanitizer(tt.config)
			if tt.wantOK {
				require.NoError(t, err)
				require.NotNil(t, sanitizer)
				assert.Equal(t, tt.config.SanitizeAllAttributes, sanitizer.sanitizeAllAttributes)
			} else {
				require.Error(t, err)
				require.Nil(t, sanitizer)
			}
		})
	}
}

func TestURLSanitizer_SanitizeAttributeURL(t *testing.T) {
	tests := []struct {
		name         string
		config       URLSanitizationConfig
		url          string
		attributeKey string
		expected     string
	}{
		{
			name: "sanitize specific attribute",
			config: URLSanitizationConfig{
				Enabled:    true,
				Attributes: []string{"http.url"},
			},
			url:          "/users/123/profile",
			attributeKey: "http.url",
			expected:     "/users/*/profile",
		},
		{
			name: "do not sanitize non-specified attribute",
			config: URLSanitizationConfig{
				Enabled:    true,
				Attributes: []string{"http.url"},
			},
			url:          "/users/123/profile",
			attributeKey: "other.url",
			expected:     "/users/123/profile",
		},
		{
			name: "sanitize all attributes enabled - specified attribute",
			config: URLSanitizationConfig{
				Enabled:               true,
				Attributes:            []string{"http.url"},
				SanitizeAllAttributes: true,
			},
			url:          "/products/456/details",
			attributeKey: "http.url",
			expected:     "/products/*/details",
		},
		{
			name: "sanitize all attributes enabled - non-specified attribute",
			config: URLSanitizationConfig{
				Enabled:               true,
				Attributes:            []string{"http.url"},
				SanitizeAllAttributes: true,
			},
			url:          "/products/456/details",
			attributeKey: "custom.url",
			expected:     "/products/*/details",
		},
		{
			name: "sanitize all attributes enabled - any attribute",
			config: URLSanitizationConfig{
				Enabled:               true,
				SanitizeAllAttributes: true,
			},
			url:          "/api/v1/orders/789",
			attributeKey: "request.path",
			expected:     "/api/v1/orders/*",
		},
		{
			name: "sanitize all attributes disabled with specific attributes",
			config: URLSanitizationConfig{
				Enabled:               true,
				Attributes:            []string{"http.url"},
				SanitizeAllAttributes: false,
			},
			url:          "/users/123/profile",
			attributeKey: "custom.url",
			expected:     "/users/123/profile",
		},
		{
			name: "disabled sanitizer",
			config: URLSanitizationConfig{
				Enabled: false,
			},
			url:          "/users/123/profile",
			attributeKey: "http.url",
			expected:     "/users/123/profile",
		},
		{
			name: "complex url with multiple path segments",
			config: URLSanitizationConfig{
				Enabled:               true,
				SanitizeAllAttributes: true,
			},
			url:          "/api/v2/users/123/orders/456/items/789",
			attributeKey: "api.path",
			expected:     "/api/v2/users/*/orders/*/items/*",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sanitizer, err := NewURLSanitizer(tt.config)
			require.NoError(t, err)
			result := sanitizer.SanitizeAttributeURL(tt.url, tt.attributeKey)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestURLSanitizer_SanitizeURL(t *testing.T) {
	config := URLSanitizationConfig{
		Enabled: true,
	}
	sanitizer, err := NewURLSanitizer(config)
	require.NoError(t, err)
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple path with numeric id",
			input:    "/users/123",
			expected: "/users/*",
		},
		{
			name:     "nested path with multiple ids",
			input:    "/users/123/orders/456",
			expected: "/users/*/orders/*",
		},
		{
			name:     "path without numeric segments",
			input:    "/api/health",
			expected: "/api/health",
		},
		{
			name:     "root path",
			input:    "/",
			expected: "/",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "complex nested path",
			input:    "/api/v1/users/123/profile/456/settings",
			expected: "/api/v1/users/*/profile/*/settings",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := sanitizer.SanitizeURL(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}
