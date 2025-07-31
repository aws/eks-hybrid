package hybrid_test

import (
	"context"
	"fmt"
	"testing"

	. "github.com/onsi/gomega"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"

	"github.com/aws/eks-hybrid/internal/api"
	"github.com/aws/eks-hybrid/internal/daemon"
	"github.com/aws/eks-hybrid/internal/node/hybrid"
)

// mockDaemonManager implements the daemon.DaemonManager interface for testing
type mockDaemonManager struct {
	status daemon.DaemonStatus
	err    error
}

func (m *mockDaemonManager) GetDaemonStatus(name string) (daemon.DaemonStatus, error) {
	return m.status, m.err
}

func (m *mockDaemonManager) StartDaemon(name string) error {
	return nil
}

func (m *mockDaemonManager) StopDaemon(name string) error {
	return nil
}

func (m *mockDaemonManager) RestartDaemon(ctx context.Context, name string, opts ...daemon.OperationOption) error {
	return nil
}

func (m *mockDaemonManager) EnableDaemon(name string) error {
	return nil
}

func (m *mockDaemonManager) DisableDaemon(name string) error {
	return nil
}

func (m *mockDaemonManager) DaemonReload() error {
	return nil
}

func (m *mockDaemonManager) Close() {}

func TestHybridNodeProvider_ValidateNodeIsInactive(t *testing.T) {
	tests := []struct {
		name                string
		kubeletDaemonStatus daemon.DaemonStatus
		daemonError         error
		expectWarning       bool
		expectedLogMsg      string
	}{
		{
			name:                "success - node is inactive",
			kubeletDaemonStatus: daemon.DaemonStatusStopped,
			expectWarning:       false,
		},
		{
			name:                "warning - kubelet daemon running",
			kubeletDaemonStatus: daemon.DaemonStatusRunning,
			expectWarning:       true,
			expectedLogMsg:      "kubelet service is still active",
		},
		{
			name:                "warning - daemon manager error",
			kubeletDaemonStatus: daemon.DaemonStatusUnknown,
			daemonError:         fmt.Errorf("daemon manager error"),
			expectWarning:       true,
			expectedLogMsg:      "daemon manager error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)

			// Create an observed zap logger
			observedZapCore, observedLogs := observer.New(zap.WarnLevel)
			observedLogger := zap.New(observedZapCore)

			// Create mock daemon manager
			mockDaemon := &mockDaemonManager{
				status: tt.kubeletDaemonStatus,
				err:    tt.daemonError,
			}

			// Create provider with observed logger
			hnp, err := hybrid.NewHybridNodeProvider(
				&api.NodeConfig{},
				[]string{"node-ip-validation", "kubelet-cert-validation", "api-server-endpoint-resolution-validation"},
				observedLogger,
				hybrid.WithDaemonManager(mockDaemon),
			)
			g.Expect(err).NotTo(HaveOccurred())

			err = hnp.Validate(context.Background())
			g.Expect(err).NotTo(HaveOccurred())

			// Check logs
			if tt.expectWarning {
				g.Expect(observedLogs.Len()).To(BeNumerically(">", 0))
				g.Expect(observedLogs.All()).To(ContainElement(
					WithTransform(func(log observer.LoggedEntry) string {
						return log.Message
					}, ContainSubstring("Validation failed")),
				))
				if tt.expectedLogMsg != "" {
					g.Expect(observedLogs.All()).To(ContainElement(
						WithTransform(func(log observer.LoggedEntry) string {
							return fmt.Sprint(log.ContextMap()["error"])
						}, ContainSubstring(tt.expectedLogMsg)),
					))
				}
			} else {
				g.Expect(observedLogs.Len()).To(BeZero())
			}
		})
	}
}
