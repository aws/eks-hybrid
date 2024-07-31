package validator

import (
	"bufio"
	"fmt"
	"os/exec"
	"runtime"
	"strings"

	"github.com/aws/eks-hybrid/internal/validation/logger"
)

type OS struct {
	Name    string
	Version string
}

var acceptedOS = map[OS]interface{}{
	{"ubuntu", "20.04"}: nil,
	{"ubuntu", "22.04"}: nil,
	{"ubuntu", "24.04"}: nil,
	{"rhel", "8"}: nil,
	{"rhel", "9"}: nil,
	{"amzn", "2023"}: nil,
}

const (
	prefixOS     = "Node OS ="
	prefixAcceptOS = "Accepted OS ="

	prefixOSName = "ID="
	prefixOSVer  = "VERSION_ID="

	// only has requirement on main version
	// no dot in the version id defined above
	specialOS = "rhel"
)

type NodeOS struct {
	GetOSType
}

type GetOSType func() (OS, error)

func DefaultNodeOS() *NodeOS {
	return NewNodeOS(findOS)
}

func NewNodeOS(s GetOSType) *NodeOS {
	return &NodeOS{s}
}

func (v *NodeOS) Validate() error {
	nodeOS, err := v.GetOSType()
	if err != nil {
		logger.Error(err, "Error getting OS")
		return err
	}
	info := fmt.Sprintf("%v | %v ", GetInfoStrString(prefixOS, nodeOS.Name+" "+nodeOS.Version), GetInfoStrString(prefixAcceptOS, buildAcceptOSInfoString()))
	
	if nodeOS.Name == specialOS {
		versionList := strings.Split(nodeOS.Version, ".")
		nodeOS.Version = versionList[0]
	}

	if _, ok := acceptedOS[nodeOS]; ok {
		logger.MarkPass(info)
		return nil
	} else {
		logger.MarkFail(info)
		return &FailError{"OS is not accepted"}
	}
}

func findOS() (OS, error) {

	var nodeOS OS

	// get OS name
	ostype := runtime.GOOS
	nodeOS.Name = ostype
	if ostype != "linux" {
		return nodeOS, nil
	}

	// Run the cat command to read the /etc/os-release file
	cmd := exec.Command("cat", "/etc/os-release")
	output, err := cmd.Output()
	if err != nil {
		return nodeOS, err
	}

	// Parse the output line by line
	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	for scanner.Scan() {
		line := scanner.Text()

		str, ok := retrieveStr(line, prefixOSName)
		if ok {
			nodeOS.Name = str
		}

		str, ok = retrieveStr(line, prefixOSVer)
		if ok {
			nodeOS.Version = str
		}
	}
	return nodeOS, nil
}

func retrieveStr(line string, prefix string) (string, bool) {
	trimStr := strings.TrimPrefix(line, prefix)
	// check whether it successfully trim the input line
	if len(trimStr) < len(line) {
		return strings.Trim(trimStr, `"'`), true
	}
	return "", false
}

// build info string for accepted OS from hash map
// Name Version_ID1/Version_ID2/Version_ID3, Name Version_ID1/Version_ID2/Version_ID3, ...
// e.g. Ubuntu 20.04/22.04/24.04, Rhel 8/9, ...
func buildAcceptOSInfoString() string {
	var acceptListString string
	for nodeOS := range acceptedOS {
		acceptListString += nodeOS.Name + " " + nodeOS.Version + ", "
	}
	return acceptListString[:len(acceptListString)-2]
}
