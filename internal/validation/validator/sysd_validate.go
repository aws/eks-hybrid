package validator

import (
	"fmt"
	"os/exec"

	"github.com/aws/eks-hybrid/internal/validation/logger"
)

const (
	recSysd   = "Recommend to use systemd"
	hasSysd   = "The system is using systemd"
	hasNoSysd = "The system is not using systemd"
)

type SysD struct {
	GetSysD
}

type GetSysD func() bool

func DefaultSysD() *SysD {
	return NewSysD(findSystemd)

}

func NewSysD(s GetSysD) *SysD {
	return &SysD{s}
}

func (v *SysD) Validate() error {
	is_sysd := v.GetSysD()

	var info string
	if !is_sysd {
		info = fmt.Sprintf("%v | %v", hasNoSysd, recSysd)
		logger.MarkWarning(info)
		return &WarningError{"systemd not found"}
	} else {
		info = fmt.Sprintf("%v | %v", hasSysd, recSysd)
		logger.MarkPass(info)
	}
	return nil
}

func findSystemd() bool {

	cmd := exec.Command("systemctl", "--version")
	_, err := cmd.Output()
	if err != nil {
		return false
	} else {
		return true
	}
}
