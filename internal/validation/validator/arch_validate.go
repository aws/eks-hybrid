package validator

import (
	"fmt"
	"runtime"

	"github.com/aws/eks-hybrid/internal/validation/logger"
)

var acceptedArch = []string{"amd64", "arm64"}

const (
	prefixArch     = "Arch ="
	prefixAcptArch = "Accepted Arch ="
)

type Arch struct {
	string
}

type GetArchType func() string

func DefaultArch() *Arch {
	return NewArch(runtime.GOARCH)
}

func NewArch(s string) *Arch {
	return &Arch{s}
}

func (v *Arch) Validate() error {
	archtype := v.string
	info := fmt.Sprintf("%v | %v ", GetInfoStrString(prefixArch, archtype), GetInfoStrString(prefixAcptArch, getAcptArchStr()))
	
	// arch is accepted
	ok := findArchExist(archtype)
	if ok {
		logger.MarkPass(info)
	} else {
		logger.MarkFail(info)
		return &FailError{"architecture is not accepted"}
	}
	return nil
}

func findArchExist(arch string) bool {
	for _, a := range acceptedArch {
		if a == arch {
			return true
		}
	}
	return false
}

func getAcptArchStr() string {
	var acptArchStr string
	for _, a := range acceptedArch {
		acptArchStr += a + ", "
	}
	return acptArchStr[:len(acptArchStr)-2]
}
