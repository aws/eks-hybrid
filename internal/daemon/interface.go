package daemon

import "context"

type Daemon interface {
	// Configure configures the daemon.
	Configure(ctx context.Context) error

	// EnsureRunning ensures that the daemon is running.
	// If the daemon is not running, it will be started.
	// If the daemon is already running, and has been re-configured, it will be restarted.
	EnsureRunning(ctx context.Context) error

	// PostLaunch runs any additional step that needs to occur after the service
	// daemon as been started
	PostLaunch() error

	// Stop stops the daemon
	// If the daemon is already stopped, this will be a no-op
	Stop() error

	// Name returns the name of the daemon.
	Name() string
}
