package securitygroup

// IngessConfig represents a security group ingress configuration
type IngessConfig struct {
	Port int `json:"port"`
}

// DefaultIngress returns the default list of ports to open
func DefaultIngress() []IngessConfig {
	return []IngessConfig{
		{
			Port: 10251,
		},
		{
			Port: 8080,
		},
		{
			Port: 9100,
		},
		{
			Port: 9403,
		},
		{
			Port: 9402,
		},
		{
			Port: 10260,
		},
	}
}
