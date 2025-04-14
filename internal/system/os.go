package system

import "github.com/go-ini/ini"

const (
	UbuntuOsName = "ubuntu"
	RhelOsName   = "rhel"
	AmazonOsName = "amzn"

	UbuntuResolvConfPath = "/run/systemd/resolve/resolv.conf"
)

// GetOsName reads the /etc/os-release file and returns the os name
func GetOsName() string {
	cfg, _ := ini.Load("/etc/os-release")
	return cfg.Section("").Key("ID").String()
}

func GetVersionCodeName() string {
	cfg, _ := ini.Load("/etc/os-release")
	return cfg.Section("").Key("VERSION_CODENAME").String()
}
