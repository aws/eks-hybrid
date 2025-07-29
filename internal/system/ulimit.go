package system

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

var ulimitOptions = map[string]string{
	"nofile": "-n",
	"nproc":  "-u",
}

// getUlimits retrieves current ulimit values for the process
func getUlimits() (uint64, uint64, error) {
	noFileLimit, err := getUlimit("nofile")
	if err != nil {
		return 0, 0, fmt.Errorf("failed to get nofile ulimit: %w", err)
	}

	nProcLimit, err := getUlimit("nproc")
	if err != nil {
		return 0, 0, fmt.Errorf("failed to get nproc ulimit: %w", err)
	}

	return noFileLimit, nProcLimit, nil
}

func getUlimit(limitType string) (uint64, error) {
	var limitUint uint64
	ulimitCmd := exec.Command("bash", "-c", fmt.Sprintf("ulimit %s", ulimitOptions[limitType]))
	output, err := ulimitCmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("failed to execute ulimit command: %w", err)
	}
	limit := strings.TrimSpace(string(output))

	if limit == "unlimited" {
		limitUint = ^uint64(0)
	} else {
		limitUint, err = strconv.ParseUint(limit, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("failed to parse ulimit value %s: %w", limit, err)
		}
	}

	return limitUint, nil
}
