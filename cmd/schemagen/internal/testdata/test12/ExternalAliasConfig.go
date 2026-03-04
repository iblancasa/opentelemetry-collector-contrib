// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package test12

import (
	"net/url"
	"os"
)

type ExternalAliasConfig struct {
	Params url.Values  `mapstructure:"params"`
	Mode   os.FileMode `mapstructure:"mode"`
}
