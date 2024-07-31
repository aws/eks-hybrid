package validator_test

import (
	"testing"

	. "github.com/onsi/gomega"

	"github.com/aws/eks-hybrid/internal/validation/validator"
)

func TestSysDValidateWarning(t *testing.T) {
	g := NewWithT(t)
	r := validator.NewRunner()
	n := validator.NewSysD(FakeSysDWarning)
	r.Register(n)

	g.Expect(r.Run()).ToNot(Succeed())
	err := r.Run()
	if err != nil {
		if customErr, ok := err.(*validator.WarningError); ok {
			t.Errorf("Received error: %v", customErr)
		}
	}
}

func TestSysDValidatePass(t *testing.T) {
	g := NewWithT(t)
	r := validator.NewRunner()
	n := validator.NewSysD(FakeSysDPass)
	r.Register(n)

	g.Expect(r.Run()).To(Succeed())
}

func FakeSysDWarning() bool {
	return false
}

func FakeSysDPass() bool {
	return true
}
