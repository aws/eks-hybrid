package network

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/aws/eks-hybrid/internal/api"
	"github.com/aws/eks-hybrid/internal/system"
	"github.com/aws/eks-hybrid/internal/validation"
)

// ProxyValidator validates proxy configuration
type ProxyValidator struct{}

// NewProxyValidator creates a new ProxyValidator
func NewProxyValidator() ProxyValidator {
	return ProxyValidator{}
}

// Run validates proxy configuration
func (v ProxyValidator) Run(ctx context.Context, informer validation.Informer, node *api.NodeConfig) error {
	var err error
	informer.Starting(ctx, "proxy-configuration", "Validating proxy configuration on the hybrid node")
	defer func() {
		informer.Done(ctx, "proxy-configuration", err)
	}()

	if err = v.Validate(node); err != nil {
		return err
	}

	return nil
}

func (v ProxyValidator) Validate(node *api.NodeConfig) error {
	if !IsProxyEnabled() {
		return nil
	}

	if err := validateProxyVariableConsistency(); err != nil {
		return err
	}

	// Skip OS-specific validations in test environment
	osName := system.GetOsName()
	if osName != "" {
		if err := validatePackageManagerProxyConfig(osName); err != nil {
			return err
		}

		if err := validateKubeletProxyConfig(); err != nil {
			return err
		}

		if err := validateContainerdProxyConfig(); err != nil {
			return err
		}

		if node.IsSSM() {
			if err := validateSSMProxyConfig(osName); err != nil {
				return err
			}
		}
	}

	return nil
}

// validateProxyVariableConsistency checks if both HTTP_PROXY and http_proxy are present and have the same value
// Same for HTTPS_PROXY and https_proxy, and NO_PROXY and no_proxy
func validateProxyVariableConsistency() error {
	proxyVars := []struct {
		upper string
		lower string
	}{
		{"HTTP_PROXY", "http_proxy"},
		{"HTTPS_PROXY", "https_proxy"},
		{"NO_PROXY", "no_proxy"},
	}

	for _, vars := range proxyVars {
		upperValue := os.Getenv(vars.upper)
		lowerValue := os.Getenv(vars.lower)
		if upperValue != "" && lowerValue != "" && upperValue != lowerValue {
			return validation.WithRemediation(
				fmt.Errorf("%s and %s environment variables have different values: %s=%s, %s=%s",
					vars.upper, vars.lower, vars.upper, upperValue, vars.lower, lowerValue),
				fmt.Sprintf("Ensure %s and %s have the same value in your environment", vars.upper, vars.lower),
			)
		}
	}

	return nil
}

// validatePackageManagerProxyConfig checks if package manager proxy configuration is valid
func validatePackageManagerProxyConfig(osName string) error {
	httpProxy := getEffectiveProxyValue("HTTP_PROXY", "http_proxy")
	httpsProxy := getEffectiveProxyValue("HTTPS_PROXY", "https_proxy")

	if httpProxy == "" && httpsProxy == "" {
		return nil
	}

	switch osName {
	case system.UbuntuOsName:
		return validateAptProxyConfig(httpProxy, httpsProxy)
	case system.RhelOsName:
		return validateYumProxyConfig(httpProxy, httpsProxy)
	case system.AmazonOsName:
		return validateDnfProxyConfig(httpProxy, httpsProxy)
	default:
		return fmt.Errorf("unsupported operating system: %s", osName)
	}
}

// validateSystemdServiceProxyConfig is a common method to validate proxy configuration for systemd services
func validateSystemdServiceProxyConfig(serviceName, servicePath string) error {
	httpProxy := getEffectiveProxyValue("HTTP_PROXY", "http_proxy")
	httpsProxy := getEffectiveProxyValue("HTTPS_PROXY", "https_proxy")
	noProxy := getEffectiveProxyValue("NO_PROXY", "no_proxy")

	if httpProxy == "" && httpsProxy == "" {
		return nil
	}

	if !fileExists(servicePath) {
		return validation.WithRemediation(
			fmt.Errorf("%s proxy configuration file not found: %s", serviceName, servicePath),
			fmt.Sprintf("Create the %s proxy configuration file at %s with the following content:\n"+
				"[Service]\n"+
				"Environment=\"HTTP_PROXY=%s\"\n"+
				"Environment=\"HTTPS_PROXY=%s\"\n"+
				"Environment=\"NO_PROXY=%s\"",
				serviceName, servicePath, httpProxy, httpsProxy, noProxy),
		)
	}

	content, err := os.ReadFile(servicePath)
	if err != nil {
		return fmt.Errorf("failed to read %s proxy configuration file: %w", serviceName, err)
	}

	if !strings.Contains(string(content), fmt.Sprintf("HTTP_PROXY=%s", httpProxy)) {
		return validation.WithRemediation(
			fmt.Errorf("%s proxy configuration file does not contain correct HTTP_PROXY value", serviceName),
			fmt.Sprintf("Update the %s proxy configuration file at %s with the correct HTTP_PROXY value: %s",
				serviceName, servicePath, httpProxy),
		)
	}

	if !strings.Contains(string(content), fmt.Sprintf("HTTPS_PROXY=%s", httpsProxy)) {
		return validation.WithRemediation(
			fmt.Errorf("%s proxy configuration file does not contain correct HTTPS_PROXY value", serviceName),
			fmt.Sprintf("Update the %s proxy configuration file at %s with the correct HTTPS_PROXY value: %s",
				serviceName, servicePath, httpsProxy),
		)
	}

	return nil
}

// validateKubeletProxyConfig checks if Kubelet has valid proxy configuration systemd unit
func validateKubeletProxyConfig() error {
	kubeletServicePath := "/etc/systemd/system/kubelet.service.d/http-proxy.conf"
	return validateSystemdServiceProxyConfig("kubelet", kubeletServicePath)
}

// validateContainerdProxyConfig checks if Containerd has valid proxy configuration systemd unit
func validateContainerdProxyConfig() error {
	containerdServicePath := "/usr/lib/systemd/system/containerd.service.d/http-proxy.conf"
	return validateSystemdServiceProxyConfig("containerd", containerdServicePath)
}

// validateSSMProxyConfig checks if SSM has valid proxy configuration systemd unit
func validateSSMProxyConfig(osName string) error {
	var ssmServicePath string
	switch osName {
	case system.UbuntuOsName:
		if isSnapSSMInstall() {
			ssmServicePath = "/etc/systemd/system/snap.amazon-ssm-agent.amazon-ssm-agent.service.d/http-proxy.conf"
		} else {
			ssmServicePath = "/etc/systemd/system/amazon-ssm-agent.service.d/http-proxy.conf"
		}
	case system.RhelOsName, system.AmazonOsName:
		ssmServicePath = "/etc/systemd/system/amazon-ssm-agent.service.d/http-proxy.conf"
	default:
		return fmt.Errorf("unsupported operating system: %s", osName)
	}

	return validateSystemdServiceProxyConfig("SSM", ssmServicePath)
}

const snapSSMAgentBinaryPath = "/snap/amazon-ssm-agent/current/amazon-ssm-agent"

func isSnapSSMInstall() bool {
	_, err := os.Stat(snapSSMAgentBinaryPath)
	return !os.IsNotExist(err)
}

// validateAptProxyConfig validates the apt proxy configuration
func validateAptProxyConfig(httpProxy, httpsProxy string) error {
	aptConfPath := "/etc/apt/apt.conf.d/proxy.conf"
	if !fileExists(aptConfPath) {
		return validation.WithRemediation(
			fmt.Errorf("apt proxy configuration file not found: %s", aptConfPath),
			fmt.Sprintf("Create the apt proxy configuration file at %s with the following content:\n"+
				"Acquire::http::Proxy \"%s\";\n"+
				"Acquire::https::Proxy \"%s\";",
				aptConfPath, httpProxy, httpsProxy),
		)
	}

	content, err := os.ReadFile(aptConfPath)
	if err != nil {
		return fmt.Errorf("failed to read apt proxy configuration file: %w", err)
	}

	if httpProxy != "" && !strings.Contains(string(content), fmt.Sprintf("Acquire::http::Proxy \"%s\"", httpProxy)) {
		return validation.WithRemediation(
			fmt.Errorf("apt proxy configuration file does not contain correct HTTP proxy setting"),
			fmt.Sprintf("Update the apt proxy configuration file at %s with the correct HTTP proxy setting: Acquire::http::Proxy \"%s\";",
				aptConfPath, httpProxy),
		)
	}

	if httpsProxy != "" && !strings.Contains(string(content), fmt.Sprintf("Acquire::https::Proxy \"%s\"", httpsProxy)) {
		return validation.WithRemediation(
			fmt.Errorf("apt proxy configuration file does not contain correct HTTPS proxy setting"),
			fmt.Sprintf("Update the apt proxy configuration file at %s with the correct HTTPS proxy setting: Acquire::https::Proxy \"%s\";",
				aptConfPath, httpsProxy),
		)
	}

	return nil
}

// validateYumProxyConfig validates the yum proxy configuration
func validateYumProxyConfig(httpProxy, httpsProxy string) error {
	yumConfPath := "/etc/yum.conf"
	if !fileExists(yumConfPath) {
		return validation.WithRemediation(
			fmt.Errorf("yum configuration file not found: %s", yumConfPath),
			fmt.Sprintf("Create the yum configuration file at %s with the following content:\n"+
				"[main]\n"+
				"proxy=%s",
				yumConfPath, httpProxy),
		)
	}

	content, err := os.ReadFile(yumConfPath)
	if err != nil {
		return fmt.Errorf("failed to read yum configuration file: %w", err)
	}

	if httpProxy != "" && !strings.Contains(string(content), fmt.Sprintf("proxy=%s", httpProxy)) {
		return validation.WithRemediation(
			fmt.Errorf("yum configuration file does not contain correct proxy setting"),
			fmt.Sprintf("Update the yum configuration file at %s with the correct proxy setting: proxy=%s",
				yumConfPath, httpProxy),
		)
	}

	return nil
}

// validateDnfProxyConfig validates the dnf proxy configuration
func validateDnfProxyConfig(httpProxy, httpsProxy string) error {
	dnfConfPath := "/etc/dnf/dnf.conf"
	if !fileExists(dnfConfPath) {
		return validation.WithRemediation(
			fmt.Errorf("dnf configuration file not found: %s", dnfConfPath),
			fmt.Sprintf("Create the dnf configuration file at %s with the following content:\n"+
				"[main]\n"+
				"proxy=%s",
				dnfConfPath, httpProxy),
		)
	}

	content, err := os.ReadFile(dnfConfPath)
	if err != nil {
		return fmt.Errorf("failed to read dnf configuration file: %w", err)
	}

	if httpProxy != "" && !strings.Contains(string(content), fmt.Sprintf("proxy=%s", httpProxy)) {
		return validation.WithRemediation(
			fmt.Errorf("dnf configuration file does not contain correct proxy setting"),
			fmt.Sprintf("Update the dnf configuration file at %s with the correct proxy setting: proxy=%s",
				dnfConfPath, httpProxy),
		)
	}

	return nil
}

func getEffectiveProxyValue(upperVar, lowerVar string) string {
	upperValue := os.Getenv(upperVar)
	lowerValue := os.Getenv(lowerVar)

	if upperValue != "" {
		return upperValue
	}
	return lowerValue
}

func fileExists(filePath string) bool {
	_, err := os.Stat(filePath)
	return !os.IsNotExist(err)
}
