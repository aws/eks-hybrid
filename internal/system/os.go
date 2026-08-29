package system

import "github.com/go-ini/ini"

const (
	UbuntuOsName = "ubuntu"
	RhelOsName   = "rhel"
	AmazonOsName = "amzn"
	SlesOsName   = "sles"

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
