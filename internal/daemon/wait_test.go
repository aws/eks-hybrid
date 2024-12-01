package daemon_test

import (
	"context"
	"sync"
	"testing"
	"time"

	. "github.com/onsi/gomega"
	"go.uber.org/zap"

	"github.com/aws/eks-hybrid/internal/daemon"
)

func TestWaitForStatusSuccess(t *testing.T) {
	ctx := context.Background()
	g := NewWithT(t)
	manager := newFakeManager()

	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	go func() {
		time.Sleep(5 * time.Millisecond)
		manager.SetStatus(daemon.DaemonStatusRunning)
	}()

	g.Expect(
		daemon.WaitForStatus(ctx, zap.NewNop(), manager, "my-daemon", daemon.DaemonStatusRunning, time.Millisecond),
	).To(Succeed())
}

func TestWaitForStatusTimeout(t *testing.T) {
	ctx := context.Background()
	g := NewWithT(t)
	manager := newFakeManager()

	ctx, cancel := context.WithTimeout(ctx, 1*time.Millisecond)
	defer cancel()

	g.Expect(
		daemon.WaitForStatus(ctx, zap.NewNop(), manager, "my-daemon", daemon.DaemonStatusRunning, 10*time.Nanosecond),
	).NotTo(MatchError((ContainSubstring("daemon my-daemon still has status unknown: context deadline exceeded"))))
}

func TestWaitForOperationTimeout(t *testing.T) {
	ctx := context.Background()
	g := NewWithT(t)
	manager := newFakeManager()

	ctx, cancel := context.WithTimeout(ctx, 1*time.Millisecond)
	defer cancel()

	g.Expect(
		daemon.WaitForOperation(ctx, manager.RestartDaemon, "my-daemon"),
	).To(MatchError(ContainSubstring("operation for daemon my-daemon did not complete in time, result is unknown")))
}

func TestWaitForOperationErrors(t *testing.T) {
	testCases := []struct {
		name    string
		result  daemon.OperationResult
		wantErr string
	}{
		{
			name:   "happy path",
			result: daemon.Done,
		},
		{
			name:   "cancelled",
			result: daemon.Canceled,
		},
		{
			name:   "dependency",
			result: daemon.Dependency,
		},
		{
			name:   "skipped",
			result: daemon.Skipped,
		},
		{
			name:    "failed",
			result:  daemon.Failed,
			wantErr: "operation for daemon my-daemon failed with result [failed]",
		},
		{
			name:    "timeout",
			result:  daemon.Timeout,
			wantErr: "operation for daemon my-daemon failed with result [timeout]",
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			g := NewWithT(t)
			ctx := context.Background()
			manager := newFakeManager()

			ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()

			go func() {
				time.Sleep(5 * time.Millisecond)
				manager.SetOperationResult("restart", "my-daemon", tc.result)
			}()

			err := daemon.WaitForOperation(ctx, manager.RestartDaemon, "my-daemon")
			if tc.wantErr != "" {
				g.Expect(err).To(MatchError(ContainSubstring(tc.wantErr)))
			} else {
				g.Expect(err).To(Succeed())
			}
		})
	}
}

func newFakeManager() *fakeManager {
	return &fakeManager{
		operationResults: make(map[string]daemon.OperationResult),
	}
}

type fakeManager struct {
	sync.RWMutex
	status           daemon.DaemonStatus
	operationResults map[string]daemon.OperationResult
}

var _ daemon.DaemonManager = &fakeManager{}

func (f *fakeManager) RestartDaemon(ctx context.Context, name string, opts ...daemon.OperationOption) error {
	resultKey := "restart-" + name
	o := &daemon.OperationOptions{}
	for _, opt := range opts {
		opt(o)
	}

	if o.Result != nil {
		go func() {
			for {
				time.Sleep(1 * time.Millisecond)
				f.RLock()
				result, ok := f.operationResults[resultKey]
				f.RUnlock()
				if ok {
					o.Result <- result
					return
				}
			}
		}()
	}

	return nil
}

func (f *fakeManager) EnableDaemon(name string) error {
	return nil
}

func (f *fakeManager) StopDaemon(name string) error {
	return nil
}

func (f *fakeManager) StartDaemon(name string) error {
	return nil
}

func (f *fakeManager) GetDaemonStatus(name string) (daemon.DaemonStatus, error) {
	f.RLock()
	defer f.RUnlock()
	return f.status, nil
}

func (f *fakeManager) DisableDaemon(name string) error {
	return nil
}

func (f *fakeManager) DaemonReload() error {
	return nil
}

func (f *fakeManager) Close() {
}

func (f *fakeManager) SetStatus(s daemon.DaemonStatus) {
	f.Lock()
	defer f.Unlock()
	f.status = s
}

func (f *fakeManager) SetOperationResult(op, name string, r daemon.OperationResult) {
	f.Lock()
	defer f.Unlock()
	f.operationResults[op+"-"+name] = r
}
