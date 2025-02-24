//go:build linux

package daemon

import (
	"context"
	"fmt"

	"github.com/coreos/go-systemd/v22/dbus"
)

var _ DaemonManager = &systemdDaemonManager{}

type systemdDaemonManager struct {
	conn *dbus.Conn
}

const (
	ModeReplace = "replace"
	TypeSymlink = "symlink"
	TypeUnlink  = "unlink"
)

func NewDaemonManager() (DaemonManager, error) {
	conn, err := dbus.NewWithContext(context.Background())
	if err != nil {
		return nil, err
	}
	return &systemdDaemonManager{
		conn: conn,
	}, nil
}

// DaemonReload instructs systemd to scan and reload all unit files
func (m *systemdDaemonManager) DaemonReload() error {
	return m.conn.ReloadContext(context.TODO())
}

func (m *systemdDaemonManager) StartDaemon(name string) error {
	unitName := getServiceUnitName(name)
	_, err := m.conn.StartUnitContext(context.TODO(), unitName, ModeReplace, nil)
	return err
}

func (m *systemdDaemonManager) StopDaemon(name string) error {
	unitName := getServiceUnitName(name)
	status, err := m.GetDaemonStatus(name)
	if err != nil {
		return err
	}
	if status == DaemonStatusRunning {
		_, err := m.conn.StopUnitContext(context.TODO(), unitName, ModeReplace, nil)
		return err
	}
	return nil
}

func (m *systemdDaemonManager) RestartDaemon(ctx context.Context, name string, opts ...OperationOption) error {
	o := &OperationOptions{
		Mode: ModeReplace,
	}
	for _, opt := range opts {
		opt(o)
	}

	resultChan := prepareResultChan(o)

	unitName := getServiceUnitName(name)
	_, err := m.conn.RestartUnitContext(ctx, unitName, o.Mode, resultChan)
	return err
}

func (m *systemdDaemonManager) GetDaemonStatus(name string) (DaemonStatus, error) {
	// TODO(g-gaston): this should take a context to it can be cancelled from the caller
	unitName := getServiceUnitName(name)
	status, err := m.conn.GetUnitPropertyContext(context.TODO(), unitName, "ActiveState")
	if err != nil {
		return DaemonStatusUnknown, err
	}
	switch status.Value.String() {
	case "\"active\"":
		return DaemonStatusRunning, nil
	case "\"inactive\"":
		return DaemonStatusStopped, nil
	default:
		return DaemonStatusUnknown, nil
	}
}

// EnableDaemon enables the daemon with the given name.
// If the daemon is already enabled, this is a no-op.
func (m *systemdDaemonManager) EnableDaemon(name string) error {
	unitName := getServiceUnitName(name)
	_, changes, err := m.conn.EnableUnitFilesContext(context.TODO(), []string{unitName}, false, false)
	if err != nil {
		return err
	}
	if len(changes) != 0 && changes[0].Type != TypeSymlink {
		return fmt.Errorf("unexpected unit file change type: %s", changes[0].Type)
	}
	return nil
}

func (m *systemdDaemonManager) DisableDaemon(name string) error {
	unitName := getServiceUnitName(name)
	changes, err := m.conn.DisableUnitFilesContext(context.TODO(), []string{unitName}, false)
	if err != nil {
		return err
	}
	if len(changes) != 1 {
		return fmt.Errorf("unexpected number of unit file changes: %d", len(changes))
	}
	if changes[0].Type != TypeUnlink {
		return fmt.Errorf("unexpected unit file change type: %s", changes[0].Type)
	}
	return nil
}

func (m *systemdDaemonManager) Close() {
	m.conn.Close()
}

func getServiceUnitName(name string) string {
	return fmt.Sprintf("%s.service", name)
}

func prepareResultChan(o *OperationOptions) chan string {
	var resultChan chan string

	if o.Result != nil {
		resultChan = make(chan string)
		go func() {
			r := <-resultChan
			o.Result <- OperationResult(r)
			close(resultChan)
		}()
	}

	return resultChan
}
