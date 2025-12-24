// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package k8sattributesprocessor

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/collector/processor/processortest"

	"github.com/open-telemetry/opentelemetry-collector-contrib/internal/k8sconfig"
	"github.com/open-telemetry/opentelemetry-collector-contrib/processor/k8sattributesprocessor/internal/metadata"
)

func TestReplicasetInformerNotCreatedWhenMetadataEmpty(t *testing.T) {
	// Test that when extract.metadata is explicitly set to empty,
	// the replicaset informer is not created (fixing issue #44708)
	
	// Create a config with explicitly empty metadata
	emptyMetadata := []string{}
	cfg := &Config{
		APIConfig: k8sconfig.APIConfig{AuthType: k8sconfig.AuthTypeServiceAccount},
		Extract: ExtractConfig{
			Metadata: &emptyMetadata, // Explicitly empty
		},
	}

	// Create processor options
	opts := createProcessorOpts(cfg)
	
	// Create processor
	kp := createKubernetesProcessor(processortest.NewNopSettings(metadata.Type), cfg, opts...)
	
	// Apply options
	for _, opt := range opts {
		assert.NoError(t, opt(kp))
	}
	
	// Verify that DeploymentName is false (should not be enabled)
	assert.False(t, kp.rules.DeploymentName, "DeploymentName should be false when metadata is explicitly empty")
	assert.False(t, kp.rules.DeploymentUID, "DeploymentUID should be false when metadata is explicitly empty")
}

func TestReplicasetInformerCreatedWhenMetadataNotSet(t *testing.T) {
	// Test that when extract.metadata is not set (nil),
	// defaults are applied and replicaset informer is created
	
	// Create a config without metadata (nil)
	cfg := &Config{
		APIConfig: k8sconfig.APIConfig{AuthType: k8sconfig.AuthTypeServiceAccount},
		Extract: ExtractConfig{
			Metadata: nil, // Not set
		},
	}

	// Create processor options - should apply defaults
	opts := createProcessorOpts(cfg)
	
	// Create processor
	kp := createKubernetesProcessor(processortest.NewNopSettings(metadata.Type), cfg, opts...)
	
	// Apply options
	for _, opt := range opts {
		assert.NoError(t, opt(kp))
	}
	
	// Verify that defaults are applied (DeploymentName should be true by default)
	defaultAttrs := enabledAttributes()
	hasDeploymentName := false
	for _, attr := range defaultAttrs {
		if attr == "k8s.deployment.name" {
			hasDeploymentName = true
			break
		}
	}
	
	if hasDeploymentName {
		assert.True(t, kp.rules.DeploymentName, "DeploymentName should be true when metadata is not set (defaults applied)")
	}
}
