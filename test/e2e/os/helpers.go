package os

import (
	"strings"

	"github.com/aws/eks-hybrid/test/e2e"
	"github.com/aws/eks-hybrid/test/e2e/constants"
)

func IsBottlerocket(name string) bool {
	return strings.HasPrefix(name, constants.BottlerocketOsName)
}

func IsUbuntu2004(name string) bool {
	return strings.HasPrefix(name, "ubuntu2004")
}

func IsUbuntu2204(name string) bool {
	return strings.HasPrefix(name, "ubuntu2204")
}

func IsUbuntu2404(name string) bool {
	return strings.HasPrefix(name, "ubuntu2404")
}

func IsRHEL8(name string) bool {
	return strings.HasPrefix(name, "rhel8")
}

func IsRHEL9(name string) bool {
	return strings.HasPrefix(name, "rhel9")
}

func GetOSFromName(name string) e2e.NodeadmOS {
	if IsBottlerocket(name) {
		return NewBottlerocket()
	} else if IsUbuntu2004(name) {
		if strings.Contains(name, "docker") {
			return NewUbuntu2004DockerSource()
		} else if strings.Contains(name, "arm64") {
			return NewUbuntu2004ARM()
		}
		return NewUbuntu2004AMD()
	} else if IsUbuntu2204(name) {
		if strings.Contains(name, "docker") {
			return NewUbuntu2204DockerSource()
		} else if strings.Contains(name, "arm64") {
			return NewUbuntu2204ARM()
		}
		return NewUbuntu2204AMD()
	} else if IsUbuntu2404(name) {
		if strings.Contains(name, "docker") {
			return NewUbuntu2404DockerSource()
		} else if strings.Contains(name, "source-none") {
			return NewUbuntu2404NoDockerSource()
		} else if strings.Contains(name, "arm64") {
			return NewUbuntu2404ARM()
		}
		return NewUbuntu2404AMD()
	} else if IsRHEL8(name) {
		if strings.Contains(name, "arm64") {
			return NewRedHat8ARM("", "")
		}
		return NewRedHat8AMD("", "")
	} else if IsRHEL9(name) {
		if strings.Contains(name, "source-none") {
			return NewRedHat9NoDockerSource("", "")
		} else if strings.Contains(name, "arm64") {
			return NewRedHat9ARM("", "")
		}
		return NewRedHat9AMD("", "")
	} else if strings.HasPrefix(name, "al23") {
		if strings.Contains(name, "arm64") {
			return NewAmazonLinux2023ARM()
		}
		return NewAmazonLinux2023AMD()
	}

	// Return nil if the OS name is not recognized
	return nil
}
