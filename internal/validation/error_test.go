package validation_test

import (
	"errors"
	"testing"

	. "github.com/onsi/gomega"

	"github.com/aws/eks-hybrid/internal/validation"
)

func TestErrorRemediationWithRemediation(t *testing.T) {
	g := NewWithT(t)
	err := errors.New("my error")
	remediable := validation.WithRemediation(err, "this is how you fix it")

	g.Expect(validation.IsRemediable(remediable)).To(BeTrue())
	g.Expect(validation.Remediation(remediable)).To(Equal("this is how you fix it"))
}

func TestIsRemediable(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "remediable",
			err:  validation.NewRemediableErr("one error", "just fix it"),
			want: true,
		},
		{
			name: "non remediable",
			err:  errors.New("non fixable"),
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			g.Expect(validation.IsRemediable(tt.err)).To(Equal(tt.want))
		})
	}
}

func TestRemediation(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "remediable",
			err:  validation.NewRemediableErr("one error", "just fix it"),
			want: "just fix it",
		},
		{
			name: "non remediable",
			err:  errors.New("non fixable"),
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			g.Expect(validation.Remediation(tt.err)).To(Equal(tt.want))
		})
	}
}

func TestFlattenRemediation(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "remediable error",
			err:  validation.NewRemediableErr("test error", "fix it"),
			want: "test error: fix it",
		},
		{
			name: "non-remediable error",
			err:  errors.New("test error"),
			want: "test error",
		},
		{
			name: "wrapped remediable error",
			err:  validation.WithRemediation(errors.New("test error"), "fix it"),
			want: "test error: fix it",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			flattened := validation.FlattenRemediation(tt.err)
			g.Expect(flattened.Error()).To(Equal(tt.want))
		})
	}
}
