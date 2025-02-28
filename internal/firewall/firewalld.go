package firewall

import (
	"fmt"
	"os/exec"
	"regexp"
)

const (
	firewalldBinary = "firewall-cmd"

	runningButFailedExitCode = 251
	notRunningExitCode       = 252
)

var firewalldActiveRegex = regexp.MustCompile(`.*running*`)

type firewalld struct {
	binPath string
}

func NewFirewalld() Manager {
	path, _ := exec.LookPath(firewalldBinary)
	return &firewalld{
		binPath: path,
	}
}

// IsEnabled returns true if firewalld is enabled and running on the node
func (fd *firewalld) IsEnabled() (bool, error) {
	// Check if firewalld is installed
	if fd.binPath != "" {
		enabledCmd := exec.Command(fd.binPath, "--state")
		out, err := enabledCmd.CombinedOutput()
		if err != nil {
			exitError, ok := err.(*exec.ExitError)
			// firewall-cmd returns non-zero exit codes for states other than running
			if ok && (exitError.ExitCode() == runningButFailedExitCode || exitError.ExitCode() == notRunningExitCode) {
				return false, nil
			}
			return false, err
		}
		// check for running status output
		if match := firewalldActiveRegex.MatchString(string(out)); match {
			return true, nil
		}
	}
	return false, nil
}

// AllowTcpPort adds a rule to the firewall to open input port
func (fd *firewalld) AllowTcpPort(port string) error {
	portAddCmd := exec.Command(fd.binPath, "--permanent", fmt.Sprintf("--add-port=%s/tcp", port))
	out, err := portAddCmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to allow port %s in firewall: %s, error: %v", port, out, err)
	}
	return nil
}

// AllowTcpPortRange adds a rule to the firewall to open the range of input port
func (fd *firewalld) AllowTcpPortRange(startPort, endPort string) error {
	portAddCmd := exec.Command(fd.binPath, "--permanent", fmt.Sprintf("--add-port=%s-%s/tcp", startPort, endPort))
	out, err := portAddCmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to allow ports in firewall: %s, error: %v", out, err)
	}
	return nil
}

// FlushRules flushes the rules and reloads the firewall to enforce the rules
func (fd *firewalld) FlushRules() error {
	reloadCmd := exec.Command(fd.binPath, "--reload")
	out, err := reloadCmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to reload firewall: %s, error: %v", out, err)
	}

	persistCmd := exec.Command(fd.binPath, "--runtime-to-permanent")
	out, err = persistCmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to persist firewall rules: %s, error: %v", out, err)
	}
	return nil
}
