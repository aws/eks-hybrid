package firewall

import (
	"bufio"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
)

const (
	ufwBinary = "ufw"

	actionAllow = "ALLOW"
)

var (
	ufwActiveRegex     = regexp.MustCompile(`.*Status: active*`)
	ufwStatusRuleRegex = regexp.MustCompile(`(\d+)\s*/(\w+)\s+(ALLOW|DENY)\s+Anywhere`)
)

type UncomplicatedFireWall struct {
	binPath string
	rules   []rule
}

type rule struct {
	port     string
	protocol string
	action   string
}

func NewUncomplicatedFirewall() Manager {
	path, _ := exec.LookPath(ufwBinary)
	return &UncomplicatedFireWall{
		binPath: path,
	}
}

// IsEnabled returns true if ufw is enabled and running on the node
func (ufw *UncomplicatedFireWall) IsEnabled() (bool, error) {
	// Check if ufw is installed
	if ufw.binPath != "" {
		statusCmd := exec.Command(ufw.binPath, "status")
		out, err := statusCmd.CombinedOutput()
		if err != nil {
			return false, fmt.Errorf("failed to get status of uncomplicated firewall: %s, error: %v", out, err)
		}
		// Check for active status output
		if match := ufwActiveRegex.MatchString(string(out)); match {
			return true, nil
		}
	}
	return false, nil
}

// AllowTcpPort adds a rule to the firewall to open input port
func (ufw *UncomplicatedFireWall) AllowTcpPort(port string) error {
	portAddCmd := exec.Command(ufw.binPath, "allow", fmt.Sprintf("%s/tcp", port))
	out, err := portAddCmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to allow port %s in firewall: %s, error: %v", port, out, err)
	}
	return nil
}

// AllowTcpPortRange adds a rule to the firewall to open the range of input port
func (ufw *UncomplicatedFireWall) AllowTcpPortRange(startPort, endPort string) error {
	portAddCmd := exec.Command(ufw.binPath, "allow", fmt.Sprintf("%s:%s/tcp", startPort, endPort))
	out, err := portAddCmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to allow ports in firewall: %s, error: %v", out, err)
	}
	return nil
}

// FlushRules flushes the rules and reloads the firewall to enforce the rules
func (ufw *UncomplicatedFireWall) FlushRules() error {
	// UFW activates the rules the moment its added, there is no need to flush them out to disk explicitly
	return nil
}

// IsPortOpen returns is port/protocol is open on the firewall
// UFW doesn't have a way to query a port, so this function refreshes the active rules
// maintained by firewall and checks if port/protocol is allowed.
func (ufw *UncomplicatedFireWall) IsPortOpen(port, protocol string) (bool, error) {
	if len(ufw.rules) == 0 {
		if err := ufw.refreshActiveRules(); err != nil {
			return false, err
		}
	}
	for _, rule := range ufw.rules {
		if rule.port == port && rule.protocol == protocol && rule.action == actionAllow {
			return true, nil
		}
	}
	return false, nil
}

func (ufw *UncomplicatedFireWall) refreshActiveRules() error {
	statusCmd := exec.Command(ufw.binPath, "status")
	out, err := statusCmd.CombinedOutput()
	if err != nil {
		return err
	}

	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		ruleLine := scanner.Text()
		matches := ufwStatusRuleRegex.FindStringSubmatch(ruleLine)
		if len(matches) > 0 {
			ufw.rules = append(ufw.rules, rule{
				port:     matches[1],
				protocol: matches[2],
				action:   matches[3],
			})
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return nil
}
