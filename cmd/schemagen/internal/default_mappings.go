// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package internal

// DefaultMappings provides core type mappings even when no .schemagen.yaml is present.
func DefaultMappings() Mappings {
	return Mappings{
		"time": PackagesMapping{
			"Time": {
				SchemaType: SchemaTypeString,
				Format:     "date-time",
			},
			"Duration": {
				SchemaType: SchemaTypeString,
				Format:     "duration",
			},
		},
		"go.opentelemetry.io/collector/config/configtelemetry": PackagesMapping{
			"Level": {
				SchemaType: SchemaTypeString,
			},
		},
	}
}

// MergeMappings merges base and overrides, with overrides taking precedence.
func MergeMappings(base, overrides Mappings) Mappings {
	out := Mappings{}
	for pkg, mapping := range base {
		out[pkg] = mapping
	}
	for pkg, mapping := range overrides {
		if out[pkg] == nil {
			out[pkg] = PackagesMapping{}
		}
		for typ, desc := range mapping {
			out[pkg][typ] = desc
		}
	}
	return out
}
