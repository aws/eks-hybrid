package validator_test

import (
	"testing"

	. "github.com/onsi/gomega"

	"github.com/aws/eks-hybrid/internal/validation/validator"
)

func TestArchValidateFail(t *testing.T) {
	g := NewWithT(t)
	r := validator.NewRunner()
	n := validator.NewArch(FakeArchFail())
	r.Register(n)

	g.Expect(r.Run()).ToNot(Succeed())
	err := r.Run()
	if err != nil {
		if customErr, ok := err.(*validator.FailError); ok {
			t.Errorf("Received error: %v", customErr)
		}
	}
}
func TestArchValidatePass(t *testing.T) {
	g := NewWithT(t)
	r := validator.NewRunner()
	n := validator.NewArch(FakeArchPass())
	r.Register(n)

	g.Expect(r.Run()).To(Succeed())
}

func FakeArchFail() string {
	return "mips"
}

func FakeArchPass() string {
	return "arm64"
}
