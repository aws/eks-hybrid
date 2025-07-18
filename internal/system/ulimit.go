package system

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// ulimit represents a system resource limit
type ulimit struct {
	// soft limit
	soft uint64
	// hard limit
	hard uint64
}

// getUlimits retrieves current ulimit values for the process
func getUlimits() (map[string]*ulimit, error) {
	limits := make(map[string]*ulimit)

	// Get ulimit values using the ulimit command
	cmd := exec.Command("sh", "-c", "ulimit -a")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to execute ulimit command: %w", err)
	}

	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		limit, err := parseUlimitLine(line)
		if err != nil {
			// Log but don't fail on individual parsing errors
			continue
		}
		if limit != nil {
			limits[limit.name] = &ulimit{
				soft: limit.value,
				hard: limit.value, // ulimit -a shows current (soft) limits
			}
		}
	}

	// Get hard limits separately
	if err := getHardLimits(limits); err != nil {
		// Don't fail if we can't get hard limits, just use soft limits
	}

	return limits, nil
}

type parsedLimit struct {
	name  string
	value uint64
}

// parseUlimitLine parses a line from ulimit -a output
func parseUlimitLine(line string) (*parsedLimit, error) {
	// Example lines:
	// core file size          (blocks, -c) 0
	// data seg size           (kbytes, -d) unlimited
	// scheduling priority             (-e) 0
	// file size               (blocks, -f) unlimited
	// pending signals                 (-i) 15679
	// max locked memory       (kbytes, -l) 65536
	// max memory size         (kbytes, -m) unlimited
	// open files                      (-n) 1024
	// pipe size            (512 bytes, -p) 8
	// POSIX message queues     (bytes, -q) 819200
	// real-time priority              (-r) 0
	// stack size              (kbytes, -s) 8192
	// cpu time               (seconds, -t) unlimited
	// max user processes              (-u) 15679
	// virtual memory          (kbytes, -v) unlimited
	// file locks                      (-x) unlimited

	parts := strings.Fields(line)
	if len(parts) < 2 {
		return nil, fmt.Errorf("invalid ulimit line format: %s", line)
	}

	// Extract the value (last field)
	valueStr := parts[len(parts)-1]
	var value uint64
	if valueStr == "unlimited" {
		value = ^uint64(0) // Max uint64 represents unlimited
	} else {
		var err error
		value, err = strconv.ParseUint(valueStr, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("failed to parse ulimit value %s: %w", valueStr, err)
		}
	}

	// Map common ulimit descriptions to standard names
	var name string
	if strings.Contains(line, "open files") {
		name = "nofile"
	} else if strings.Contains(line, "max user processes") {
		name = "nproc"
	} else if strings.Contains(line, "core file size") {
		name = "core"
	} else if strings.Contains(line, "stack size") {
		name = "stack"
	} else if strings.Contains(line, "max memory size") {
		name = "rss"
	} else if strings.Contains(line, "virtual memory") {
		name = "as"
	} else if strings.Contains(line, "file size") {
		name = "fsize"
	} else if strings.Contains(line, "cpu time") {
		name = "cpu"
	} else if strings.Contains(line, "max locked memory") {
		name = "memlock"
	} else {
		// Skip unknown limits
		return nil, nil
	}

	return &parsedLimit{
		name:  name,
		value: value,
	}, nil
}

// getHardLimits attempts to get hard limits by reading /proc/self/limits
func getHardLimits(limits map[string]*ulimit) error {
	file, err := os.Open("/proc/self/limits")
	if err != nil {
		return fmt.Errorf("failed to open /proc/self/limits: %w", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	// Skip header line
	if scanner.Scan() {
		// Skip the header
	}

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		name, soft, hard, err := parseProcLimitsLine(line)
		if err != nil {
			continue
		}

		if existingLimit, exists := limits[name]; exists {
			existingLimit.soft = soft
			existingLimit.hard = hard
		} else {
			limits[name] = &ulimit{
				soft: soft,
				hard: hard,
			}
		}
	}

	return scanner.Err()
}

// parseProcLimitsLine parses a line from /proc/self/limits
func parseProcLimitsLine(line string) (string, uint64, uint64, error) {
	// Example line:
	// Max open files            1024                 4096                 files
	fields := strings.Fields(line)
	if len(fields) < 4 {
		return "", 0, 0, fmt.Errorf("invalid /proc/limits line format: %s", line)
	}

	// Map limit names from /proc/self/limits to standard names
	var name string
	limitName := strings.Join(fields[:len(fields)-3], " ")
	switch {
	case strings.Contains(limitName, "Max open files"):
		name = "nofile"
	case strings.Contains(limitName, "Max processes"):
		name = "nproc"
	case strings.Contains(limitName, "Max core file size"):
		name = "core"
	case strings.Contains(limitName, "Max stack size"):
		name = "stack"
	case strings.Contains(limitName, "Max resident set"):
		name = "rss"
	case strings.Contains(limitName, "Max address space"):
		name = "as"
	case strings.Contains(limitName, "Max file size"):
		name = "fsize"
	case strings.Contains(limitName, "Max cpu time"):
		name = "cpu"
	case strings.Contains(limitName, "Max locked memory"):
		name = "memlock"
	default:
		return "", 0, 0, fmt.Errorf("unknown limit: %s", limitName)
	}

	softStr := fields[len(fields)-3]
	hardStr := fields[len(fields)-2]

	var soft, hard uint64
	if softStr == "unlimited" {
		soft = ^uint64(0)
	} else {
		var err error
		soft, err = strconv.ParseUint(softStr, 10, 64)
		if err != nil {
			return "", 0, 0, fmt.Errorf("failed to parse soft limit %s: %w", softStr, err)
		}
	}

	if hardStr == "unlimited" {
		hard = ^uint64(0)
	} else {
		var err error
		hard, err = strconv.ParseUint(hardStr, 10, 64)
		if err != nil {
			return "", 0, 0, fmt.Errorf("failed to parse hard limit %s: %w", hardStr, err)
		}
	}

	return name, soft, hard, nil
}
