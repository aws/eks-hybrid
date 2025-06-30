package api

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/runtime"
)

func TestIsFailSwapOnDisabled(t *testing.T) {
	tests := []struct {
		name           string
		nodeConfig     *NodeConfig
		expectedResult bool
		expectError    bool
	}{
		{
			name:           "nil node config",
			nodeConfig:     nil,
			expectedResult: false,
			expectError:    false,
		},
		{
			name: "empty kubelet config",
			nodeConfig: &NodeConfig{
				Spec: NodeConfigSpec{
					Kubelet: KubeletOptions{
						Config: InlineDocument{},
					},
				},
			},
			expectedResult: false,
			expectError:    false,
		},
		{
			name: "failSwapOn explicitly set to true",
			nodeConfig: &NodeConfig{
				Spec: NodeConfigSpec{
					Kubelet: KubeletOptions{
						Config: InlineDocument{
							"failSwapOn": runtime.RawExtension{
								Raw: []byte("true"),
							},
						},
					},
				},
			},
			expectedResult: false,
			expectError:    false,
		},
		{
			name: "failSwapOn explicitly set to false",
			nodeConfig: &NodeConfig{
				Spec: NodeConfigSpec{
					Kubelet: KubeletOptions{
						Config: InlineDocument{
							"failSwapOn": runtime.RawExtension{
								Raw: []byte("false"),
							},
						},
					},
				},
			},
			expectedResult: true,
			expectError:    false,
		},
		{
			name: "failSwapOn with invalid JSON",
			nodeConfig: &NodeConfig{
				Spec: NodeConfigSpec{
					Kubelet: KubeletOptions{
						Config: InlineDocument{
							"failSwapOn": runtime.RawExtension{
								Raw: []byte("invalid-json"),
							},
						},
					},
				},
			},
			expectedResult: false,
			expectError:    true,
		},
		{
			name: "other kubelet config without failSwapOn",
			nodeConfig: &NodeConfig{
				Spec: NodeConfigSpec{
					Kubelet: KubeletOptions{
						Config: InlineDocument{
							"maxPods": runtime.RawExtension{
								Raw: []byte("110"),
							},
						},
					},
				},
			},
			expectedResult: false,
			expectError:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := IsFailSwapOnDisabled(tt.nodeConfig)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}

			assert.Equal(t, tt.expectedResult, result)
		})
	}
}

// Helper function to create a runtime.RawExtension from a boolean value
func createBoolRawExtension(value bool) runtime.RawExtension {
	raw, _ := json.Marshal(value)
	return runtime.RawExtension{Raw: raw}
}

func TestIsFailSwapOnDisabled_WithHelperFunction(t *testing.T) {
	// Test using the helper function for cleaner test setup
	nodeConfigWithFailSwapOnFalse := &NodeConfig{
		Spec: NodeConfigSpec{
			Kubelet: KubeletOptions{
				Config: InlineDocument{
					"failSwapOn": createBoolRawExtension(false),
				},
			},
		},
	}

	result, err := IsFailSwapOnDisabled(nodeConfigWithFailSwapOnFalse)
	assert.NoError(t, err)
	assert.True(t, result)
}
