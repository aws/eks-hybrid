package firewall

// Manager is an interface for providing firewall functionalities
type Manager interface {
	// IsEnabled returns if firewall is enabled
	IsEnabled() (bool, error)

	// AllowTcpPort adds a rule to open a port on the host
	AllowTcpPort(string) error

	// AllowTcpPortRange adds a rule to open a range of port on the host
	AllowTcpPortRange(string, string) error

	// FlushRules writes newly added rules to disk and reloads the firewall
	FlushRules() error

	// IsPortOpen return true if firewall allows traffic on input port
	IsPortOpen(string, string) (bool, error)
}
