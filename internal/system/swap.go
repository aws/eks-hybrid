package system

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"strings"

	"go.uber.org/zap"

	"github.com/aws/eks-hybrid/internal/api"
)

const (
	swapAspectName    = "swap"
	swapTypePartition = "partition"
	swapTypeFile      = "file"
)

type swapAspect struct {
	nodeConfig *api.NodeConfig
	logger     *zap.Logger
}

var _ SystemAspect = &swapAspect{}

func NewSwapAspect(cfg *api.NodeConfig, logger *zap.Logger) SystemAspect {
	return &swapAspect{nodeConfig: cfg, logger: logger}
}

func (s *swapAspect) Name() string {
	return swapAspectName
}

func (s *swapAspect) Setup() error {
	hasSwapPartition, err := partitionSwapExists()
	if err != nil {
		return err
	}
	if hasSwapPartition {
		return fmt.Errorf("failed to disable swap: partition type swap found on the host")
	}
	if err = s.swapOff(); err != nil {
		return err
	}
	return disableSwapOnFstab()
}

// Check if there are swaps of type partition exist on host because currently
// nodeadm can only disable file type swap, if it's partition type, nodeadm
// can only temporarily disable the swap, and swap will come back after host reboot.
// If partition type swap exists, user needs to manually remove the partition swap before
// running nodeadm init.
func partitionSwapExists() (bool, error) {
	swapfiles, err := getSwapfilePaths()
	if err != nil {
		return false, err
	}
	for _, swap := range swapfiles {
		if swap.swapType == swapTypePartition {
			return true, nil
		}
	}
	return false, nil
}

func (s *swapAspect) swapOff() error {
	swapfiles, err := getSwapfilePaths()
	if err != nil {
		return err
	}
	for _, swap := range swapfiles {
		path := swap.filePath
		if swap.swapType == swapTypePartition {
			return fmt.Errorf("partition type swapfile %s found in /proc/swaps, please remove the swapfile", swap.filePath)
		}
		if _, err := os.Stat(path); err == nil {
			s.logger.Info("Disabling swap...", zap.Reflect("swapfile path", path))
			offCmd := exec.Command("swapoff", path)
			out, err := offCmd.CombinedOutput()
			if err != nil {
				return fmt.Errorf("failed to turn of swap on %s, command output: %s, %v", path, out, err)
			}
		} else if errors.Is(err, fs.ErrNotExist) {
			// path to swapfile does not exist
			s.logger.Warn("swapfile path does not exists", zap.Reflect("swapfile path", path))
		} else {
			// file may or may not exist. See err for details.
			return fmt.Errorf("unexpced error while trying to open /proc/swaps file: %v", err)
		}
	}
	return nil
}

// Read swapfile paths from /proc/fstab file and return them as a list of string
// If there is no /proc/swaps file, returns a nil slice
// /proc/fstab file format will be like:
// Filename                          Type         Size     Used    Priority
// <path-to-swap-file>   	     file/partition   524280   0       -1
func getSwapfilePaths() ([]*swap, error) {
	var paths []*swap
	file, err := os.OpenFile("/proc/swaps", os.O_RDONLY, 0o444)
	if err != nil {
		if os.IsNotExist(err) {
			return paths, nil
		}
		return paths, err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		if lineNo == 1 {
			continue
		}
		swap, err := parseProcSwapsLine(scanner.Text())
		if err != nil {
			return nil, fmt.Errorf("/proc/swaps file syntax error at line %d: %s", lineNo, err)
		}
		paths = append(paths, swap)
	}
	return paths, nil
}

type swap struct {
	// path of swap file
	filePath string
	// type of swap, value is one of partition and file
	swapType string
}

// Mount represents the filesystem info
type mount struct {
	// The block special device or remote filesystem to be mounted
	spec string

	// The mount point for the filesystem
	file string

	// The type of the filesystem
	vfsType string
}

func disableSwapOnFstab() error {
	file, err := os.OpenFile("/etc/fstab", os.O_RDWR, 0o644)
	if err != nil {
		return err
	}
	defer file.Close()

	var bs []byte
	buf := bytes.NewBuffer(bs)
	scanner := bufio.NewScanner(file)
	lineNo := 0

	for scanner.Scan() {
		lineNo++
		fstabMount, err := parseFstabLine(scanner.Text())
		if err != nil {
			return fmt.Errorf("/etc/fstab syntax error at line %d: %s", lineNo, err)
		}
		if fstabMount == nil || fstabMount != nil && fstabMount.vfsType != "swap" {
			buf.WriteString(scanner.Text() + "\n")
		}
	}
	if err := file.Truncate(0); err != nil {
		return err
	}
	if _, err := file.Seek(0, 0); err != nil {
		return err
	}
	_, err = buf.WriteTo(file)
	return err
}

func parseFstabLine(line string) (*mount, error) {
	line = strings.TrimSpace(line)

	// Lines starting with a pound sign (#) are comments, and are ignored. So are empty lines.
	if (line == "") || (line[0] == '#') {
		return nil, nil
	}

	fields := strings.Fields(line)
	fstabMount := &mount{}
	if len(fields) < 3 {
		return nil, fmt.Errorf("too few fields (%d), at least 4 are expected", len(fields))
	} else {
		fstabMount.spec = fields[0]
		fstabMount.file = fields[1]
		fstabMount.vfsType = fields[2]
	}

	return fstabMount, nil
}

func parseProcSwapsLine(line string) (*swap, error) {
	line = strings.TrimSpace(line)
	if (line == "") || (line[0] == '#') {
		return nil, nil
	}
	fields := strings.Fields(line)
	if len(fields) != 5 {
		return nil, fmt.Errorf("Error in /proc/swaps file, line (%s) has %d fields, 5 are expected", line, len(fields))
	}
	return &swap{
		filePath: fields[0],
		swapType: fields[1],
	}, nil
}
