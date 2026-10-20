package network

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/aws/eks-hybrid/internal/api"
	"github.com/aws/eks-hybrid/internal/system"
	"github.com/aws/eks-hybrid/internal/validation"
)

func TestIsProxyEnabled(t *testing.T) {
	tests := []struct {
		name     string
		envVars  map[string]string
		expected bool
	}{
		{
			name:     "no proxy env vars",
			envVars:  map[string]string{},
			expected: false,
		},
		{
			name: "HTTP_PROXY set",
			envVars: map[string]string{
				"HTTP_PROXY": "http://proxy.example.com:8080",
			},
			expected: true,
		},
		{
			name: "http_proxy set",
			envVars: map[string]string{
				"http_proxy": "http://proxy.example.com:8080",
			},
			expected: true,
		},
		{
			name: "HTTPS_PROXY set",
			envVars: map[string]string{
				"HTTPS_PROXY": "http://proxy.example.com:8080",
			},
			expected: true,
		},
		{
			name: "https_proxy set",
			envVars: map[string]string{
				"https_proxy": "http://proxy.example.com:8080",
			},
			expected: true,
		},
		{
			name: "NO_PROXY set",
			envVars: map[string]string{
				"NO_PROXY": "localhost,127.0.0.1",
			},
			expected: false,
		},
		{
			name: "no_proxy set",
			envVars: map[string]string{
				"no_proxy": "localhost,127.0.0.1",
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			os.Clearenv()

			for k, v := range tt.envVars {
				os.Setenv(k, v)
			}

			result := IsProxyEnabled()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestValidateProxyVariableConsistency(t *testing.T) {
	tests := []struct {
		name          string
		envVars       map[string]string
		expectedError bool
	}{
		{
			name:          "no proxy env vars",
			envVars:       map[string]string{},
			expectedError: false,
		},
		{
			name: "HTTP_PROXY and http_proxy match",
			envVars: map[string]string{
				"HTTP_PROXY": "http://proxy.example.com:8080",
				"http_proxy": "http://proxy.example.com:8080",
			},
			expectedError: false,
		},
		{
			name: "HTTP_PROXY and http_proxy mismatch",
			envVars: map[string]string{
				"HTTP_PROXY": "http://proxy.example.com:8080",
				"http_proxy": "http://different.example.com:8080",
			},
			expectedError: true,
		},
		{
			name: "HTTPS_PROXY and https_proxy match",
			envVars: map[string]string{
				"HTTPS_PROXY": "http://proxy.example.com:8080",
				"https_proxy": "http://proxy.example.com:8080",
			},
			expectedError: false,
		},
		{
			name: "HTTPS_PROXY and https_proxy mismatch",
			envVars: map[string]string{
				"HTTPS_PROXY": "http://proxy.example.com:8080",
				"https_proxy": "http://different.example.com:8080",
			},
			expectedError: true,
		},
		{
			name: "NO_PROXY and no_proxy match",
			envVars: map[string]string{
				"NO_PROXY": "localhost,127.0.0.1",
				"no_proxy": "localhost,127.0.0.1",
			},
			expectedError: false,
		},
		{
			name: "NO_PROXY and no_proxy mismatch",
			envVars: map[string]string{
				"NO_PROXY": "localhost,127.0.0.1",
				"no_proxy": "localhost",
			},
			expectedError: true,
		},
		{
			name: "only HTTP_PROXY set",
			envVars: map[string]string{
				"HTTP_PROXY": "http://proxy.example.com:8080",
			},
			expectedError: false,
		},
		{
			name: "only http_proxy set",
			envVars: map[string]string{
				"http_proxy": "http://proxy.example.com:8080",
			},
			expectedError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			os.Clearenv()

			for k, v := range tt.envVars {
				os.Setenv(k, v)
			}

			err := validateProxyVariableConsistency()
			if tt.expectedError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestGetEffectiveProxyValue(t *testing.T) {
	tests := []struct {
		name     string
		envVars  map[string]string
		upperVar string
		lowerVar string
		expected string
	}{
		{
			name:     "no proxy env vars",
			envVars:  map[string]string{},
			upperVar: "HTTP_PROXY",
			lowerVar: "http_proxy",
			expected: "",
		},
		{
			name: "only upper var set",
			envVars: map[string]string{
				"HTTP_PROXY": "http://proxy.example.com:8080",
			},
			upperVar: "HTTP_PROXY",
			lowerVar: "http_proxy",
			expected: "http://proxy.example.com:8080",
		},
		{
			name: "only lower var set",
			envVars: map[string]string{
				"http_proxy": "http://proxy.example.com:8080",
			},
			upperVar: "HTTP_PROXY",
			lowerVar: "http_proxy",
			expected: "http://proxy.example.com:8080",
		},
		{
			name: "both vars set, upper takes precedence",
			envVars: map[string]string{
				"HTTP_PROXY": "http://upper.example.com:8080",
				"http_proxy": "http://lower.example.com:8080",
			},
			upperVar: "HTTP_PROXY",
			lowerVar: "http_proxy",
			expected: "http://upper.example.com:8080",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			os.Clearenv()

			for k, v := range tt.envVars {
				os.Setenv(k, v)
			}

			result := getEffectiveProxyValue(tt.upperVar, tt.lowerVar)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestFileExists(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "fileexists-test")
	if err != nil {
		t.Fatalf("Failed to create temporary file: %v", err)
	}
	defer os.Remove(tmpFile.Name())
	tmpFile.Close()

	tests := []struct {
		name     string
		filePath string
		expected bool
	}{
		{
			name:     "file exists",
			filePath: tmpFile.Name(),
			expected: true,
		},
		{
			name:     "file does not exist",
			filePath: "/path/to/nonexistent/file",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := fileExists(tt.filePath)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestValidateProxyConfig(t *testing.T) {
	os.Clearenv()

	nodeConfig := &api.NodeConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name: "my-node",
		},
		Spec: api.NodeConfigSpec{
			Hybrid: &api.HybridOptions{
				SSM: &api.SSM{
					ActivationCode: "activation-code",
					ActivationID:   "activation-id",
				},
			},
		},
	}

	validator := NewProxyValidator()
	err := validator.Validate(nodeConfig)
	assert.NoError(t, err)
}

func TestValidateSystemdServiceProxyConfig(t *testing.T) {
	os.Clearenv()

	os.Setenv("HTTP_PROXY", "http://proxy.example.com:8080")
	os.Setenv("HTTPS_PROXY", "http://proxy.example.com:8080")

	err := validateSystemdServiceProxyConfig("test-service", "/path/to/nonexistent/file")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "test-service proxy configuration file not found")

	assert.True(t, validation.IsRemediable(err))
	assert.NotEmpty(t, validation.Remediation(err))
}

func TestValidatePackageManagerProxyConfigSupportsOracleLinux(t *testing.T) {
	os.Clearenv()
	os.Setenv("HTTP_PROXY", "http://proxy.example.com:8080")

	err := validatePackageManagerProxyConfig(system.OracleLinuxOsName)
	if err != nil {
		assert.NotContains(t, err.Error(), "unsupported operating system")
	}
}

func TestValidateSSMProxyConfigSupportsOracleLinux(t *testing.T) {
	os.Clearenv()
	os.Setenv("HTTP_PROXY", "http://proxy.example.com:8080")

	err := validateSSMProxyConfig(system.OracleLinuxOsName)
	if err != nil {
		assert.NotContains(t, err.Error(), "unsupported operating system")
	}
}
