package peered

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/go-logr/logr"
	"github.com/onsi/gomega"

	"github.com/aws/eks-hybrid/test/e2e"
	"github.com/aws/eks-hybrid/test/e2e/ssh"
)

// SerialOutputBlock is a helper to run sections of a ginkgo test while outputting the serial console output of an instance.
// It paused the main test logs while streaming the serial console output, and resumes them once the test "body" is done.
// The serial console output is also saved to a file until Close is called, no matter if you are running a test block or not.
// This is very useful to help debugging issues with the node joining the cluster, specially if the process runs as part of the node initialization.
type SerialOutputBlock struct {
	by           func(description string, callback ...func())
	serial       *ssh.SerialConsole
	logsFile     io.WriteCloser
	serialOutout *e2e.SwitchWriter
	testLogger   e2e.PausableLogger
	stdOut       io.Writer
}

type SerialOutputConfig struct {
	By         func(description string, callback ...func())
	PeeredNode *Node
	Instance   PeeredInstance
	TestLogger e2e.PausableLogger
	OutputFile string
	Output     io.Writer // Writer to output serial console to, defaults to os.Stdout if nil
}

func NewSerialOutputBlock(ctx context.Context, config *SerialOutputConfig) (*SerialOutputBlock, error) {
	serial, err := config.PeeredNode.SerialConsole(ctx, config.Instance.ID)
	if err != nil {
		return nil, fmt.Errorf("preparing EC2 for serial connection: %w", err)
	}

	output := config.Output
	if output == nil {
		output = os.Stdout
	}

	pausableOutput := e2e.NewSwitchWriter(output)
	pausableOutput.Pause() // We start it paused, we will resume it once the test output is paused
	file, err := os.Create(config.OutputFile)
	if err != nil {
		return nil, fmt.Errorf("creating file to store serial output: %w", err)
	}

	if err := serial.Copy(io.MultiWriter(pausableOutput, file)); err != nil {
		return nil, fmt.Errorf("connecting to EC2 serial console: %w", err)
	}

	return &SerialOutputBlock{
		by:           config.By,
		serial:       serial,
		logsFile:     file,
		testLogger:   config.TestLogger,
		serialOutout: pausableOutput,
		stdOut:       output,
	}, nil
}

// It runs the test body while streaming the serial console output of the instance to stdout.
// It pauses the main test logs while streaming the serial console output, and resumes them once the test "body" is done.
// This actually doesn't create a ginkgo node, it just uses By to print the description and help distinguish this
// test block in the logs.
func (b *SerialOutputBlock) It(description string, body func()) {
	// This ensures that test logs are restored even if this method panics
	// Both Paused and Resume are idempotent
	defer func() {
		b.serialOutout.Pause()
		gomega.Expect(b.testLogger.Resume()).To(gomega.Succeed())
	}()

	b.by(description, func() {
		b.testLogger.Info(
			fmt.Sprintf("Streaming Node serial output while the node %s. Test logs are paused in the meantime and will resume later.", description),
		)
		b.testLogger.Pause()
		// hack: this prints the resume message immediately after resuming the test logs
		// and more importantly, before any logs produced by body()
		b.testLogger.Info("Node serial output stopped and test logs resumed")
		fmt.Fprintln(b.stdOut, "-------------- Serial output starts here --------------")
		gomega.Expect(b.serialOutout.Resume()).To(gomega.Succeed())
		body()
		b.serialOutout.Pause()
		fmt.Fprintln(b.stdOut, "-------------- Serial output ends here --------------")
		gomega.Expect(b.testLogger.Resume()).To(gomega.Succeed())
	})
}

func (b *SerialOutputBlock) Close() {
	b.serial.Close()
	b.logsFile.Close()
}

// serialOutoutBlockNoop is a no-op implementation of SerialOutputBlock.
// Useful as a fallback when serial console is not available.
// It allows to return the same shape as the real SerialOutputBlock,
// so the caller doesn't need to check for nil and can freely use it as
// if it was the real thing. The only difference is that it won't actually
// stream the serial output and it will keep the test logs unpaused.
type serialOutoutBlockNoop struct {
	testLogger logr.Logger
	by         func(description string, callback ...func())
}

func (b serialOutoutBlockNoop) It(description string, body func()) {
	b.by(description, func() {
		b.testLogger.Info("Serial console not available, skipping serial output stream and leaving test logs unpaused.")
		body()
	})
}

func (b serialOutoutBlockNoop) Close() {}

type ItBlockCloser interface {
	It(description string, body func())
	Close()
}

// NewSerialOutputBlockBestEffort creates a SerialOutputBlock if the serial console is available, otherwise it returns a no-op implementation.
func NewSerialOutputBlockBestEffort(ctx context.Context, config *SerialOutputConfig) ItBlockCloser {
	block, err := NewSerialOutputBlock(ctx, config)
	if err != nil {
		return serialOutoutBlockNoop{
			testLogger: config.TestLogger.Logger,
			by:         config.By,
		}
	}

	return block
}
