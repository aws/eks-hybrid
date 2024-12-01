package validation_test

import (
	"testing"

	. "github.com/onsi/gomega"

	"github.com/aws/eks-hybrid/internal/validation"
)

func TestChannelReader(t *testing.T) {
	t.Run("reads one line", func(t *testing.T) {
		g := NewWithT(t)
		ch := make(chan string, 1)
		ch <- "line 1"
		close(ch)

		r := validation.NewChannelReader(ch, "test")
		line, ok := r.Line()
		g.Expect(ok).To(BeTrue())
		g.Expect(line).To(Equal("line 1"))

		_, ok = r.Line()
		g.Expect(ok).To(BeFalse())
	})

	t.Run("reads multiple lines", func(t *testing.T) {
		g := NewWithT(t)
		ch := make(chan string, 2)
		ch <- "line 1"
		ch <- "line 2"
		close(ch)

		r := validation.NewChannelReader(ch, "test")
		line, ok := r.Line()
		g.Expect(ok).To(BeTrue())
		g.Expect(line).To(Equal("line 1"))

		line, ok = r.Line()
		g.Expect(ok).To(BeTrue())
		g.Expect(line).To(Equal("line 2"))

		_, ok = r.Line()
		g.Expect(ok).To(BeFalse())
	})

	t.Run("reads from empty channel", func(t *testing.T) {
		g := NewWithT(t)
		ch := make(chan string)

		r := validation.NewChannelReader(ch, "test")
		_, ok := r.Line()
		g.Expect(ok).To(BeFalse())
		close(ch)

		_, ok = r.Line()
		g.Expect(ok).To(BeFalse())
	})
}

func TestFileCapture(t *testing.T) {
}

func TestFileCaptureInit(t *testing.T) {
	g := NewWithT(t)
	out := make(chan string, 3)
	fileCapture := validation.NewFileCapture(out)

	err := fileCapture.Init()
	g.Expect(err).To(BeNil())
	g.Expect(fileCapture.File).NotTo(BeNil())

	_, err = fileCapture.File.WriteString("line 1\n")
	g.Expect(err).To(BeNil())
	_, err = fileCapture.File.WriteString("line 2\n")
	g.Expect(err).To(BeNil())
	_, err = fileCapture.File.WriteString("line 3\n")
	g.Expect(err).To(BeNil())

	g.Expect(<-out).To(Equal("line 1"))
	g.Expect(<-out).To(Equal("line 2"))
	g.Expect(<-out).To(Equal("line 3"))

	g.Expect(fileCapture.Close()).To(Succeed())
}
