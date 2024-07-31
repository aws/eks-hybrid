package validator_test

import (
	"testing"

	. "github.com/onsi/gomega"

	"github.com/aws/eks-hybrid/internal/validation/validator"
)

func TestOSValidateFail(t *testing.T) {
	g := NewWithT(t)
	r := validator.NewRunner()
	n := validator.NewNodeOS(FakeOSFail)
	r.Register(n)

	g.Expect(r.Run()).ToNot(Succeed())
	err := r.Run()
	if err != nil {
		if customErr, ok := err.(*validator.FailError); ok {
			t.Errorf("Received error: %v", customErr)
		}
	}
}
func TestOSValidatePass(t *testing.T) {
	g := NewWithT(t)
	r := validator.NewRunner()
	n := validator.NewNodeOS(FakeOSPass)
	r.Register(n)

	g.Expect(r.Run()).To(Succeed())
}

func FakeOSFail() (validator.OS, error) {

	return validator.OS{"ubuntu", "18.04"}, nil
}

func FakeOSPass() (validator.OS, error) {
	return validator.OS{"ubuntu", "20.04"}, nil
}
