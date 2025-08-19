package system

import (
	"fmt"
	"os"

	"github.com/go-ini/ini"
	"go.uber.org/zap"
)

const (
	UbuntuOsName = "ubuntu"
	RhelOsName   = "rhel"
	AmazonOsName = "amzn"

	UbuntuResolvConfPath = "/run/systemd/resolve/resolv.conf"
)

// GetOsName reads the /etc/os-release file and returns the os name
func GetOsName() string {
	cfg, _ := ini.Load("/etc/os-release")
	if cfg != nil {
		return cfg.Section("").Key("ID").String()
	}
	return ""
}

func GetVersionCodeName() string {
	cfg, _ := ini.Load("/etc/os-release")
	return cfg.Section("").Key("VERSION_CODENAME").String()
}

// SetupRHELJournalCompatibility creates a symlink from /var/log/journal to /run/log/journal
// on RHEL nodes to ensure compatibility with systemd-journald logging.
func SetupRHELJournalCompatibility(logger *zap.Logger) error {
	if GetOsName() == RhelOsName {
		symlinkPath := "/var/log/journal"
		targetPath := "/run/log/journal"

		// Check if symlink already exists
		if _, err := os.Lstat(symlinkPath); os.IsNotExist(err) {
			if err := os.MkdirAll("/var/log", 0o755); err != nil {
				return fmt.Errorf("failed to create /var/log directory: %w", err)
			}

			if err := os.Symlink(targetPath, symlinkPath); err != nil {
				return fmt.Errorf("failed to create journal symlink: %w", err)
			}

			logger.Info("Created RHEL journal compatibility symlink",
				zap.String("symlink", symlinkPath),
				zap.String("target", targetPath))
		}
	}
	return nil
}
