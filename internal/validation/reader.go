package validation

import (
	"bufio"
	"os"
)

// LineReader is an interface that allows to read lines from some source.
type LineReader interface {
	// Line reads a line from the source. It returns false for the second argument
	// if there is no more lines to read.
	Line() (string, bool)
	// Name is name of the lines source.
	Name() string
}

// ChannelReader implements LineReader for a channel of strings source.
type ChannelReader struct {
	in   <-chan string
	name string
}

var _ LineReader = ChannelReader{}

// NewChannelReader returns a new ChannelReader.
func NewChannelReader(in <-chan string, name string) ChannelReader {
	return ChannelReader{in: in, name: name}
}

// Line reads a line from the channel.
// Returns false if there is no more lines to read (in buffered channels)
// or the channel is closed.
func (c ChannelReader) Line() (string, bool) {
	select {
	case line, ok := <-c.in:
		if !ok {
			// Channel is closed
			return "", false
		}

		return line, true

	default:
		// Nothing more in the channel to read
		return "", false
	}
}

func (c ChannelReader) Name() string {
	return c.name
}

// FileCapture forwards the content of a file to a channel,
// reading line by line.
type FileCapture struct {
	*os.File
	done    chan struct{}
	scanner *bufio.Scanner
	out     chan<- string
}

// NewFileCapture returns a new FileCapture.
func NewFileCapture(out chan<- string) *FileCapture {
	r := &FileCapture{
		out: out,
	}
	return r
}

// Init initializes the FileCapture.
// The caller is responsible for calling Close
// after Init succeeds once they are done with the FileCapture.
// If not, FileCapture might leak resources.
func (r *FileCapture) Init() error {
	readPipe, writePipe, err := os.Pipe()
	if err != nil {
		return err
	}

	r.File = writePipe
	r.done = make(chan struct{})
	r.scanner = bufio.NewScanner(readPipe)

	go func() {
		for r.scanner.Scan() {
			r.out <- r.scanner.Text()
		}
		close(r.out)
		close(r.done)
	}()

	return nil
}

// Close closes all internal resources and stops routines.
func (r *FileCapture) Close() error {
	// Close the write pipe to signal end of input
	r.File.Close()

	// Wait for the reading goroutine to finish
	<-r.done

	if err := r.scanner.Err(); err != nil {
		return err
	}

	return nil
}

// PrinterWithStdCapture is a convenience struct that combines a Printer
// and a FileCapture, allowing to capture a os.File. Useful for
// capturing stderr or stdout and displaying it through the Printer.
type PrinterWithStdCapture struct {
	Printer
	FileCapture
}

// NewPrinterWithStdCapture returns a new PrinterWithStdCapture.
func NewPrinterWithStdCapture(stdName string, noColor bool) *PrinterWithStdCapture {
	out := make(chan string, 100)
	opts := []PrinterOpt{
		WithExternalLogs(NewChannelReader(out, stdName)),
	}
	if noColor {
		opts = append(opts, WithNoColor())
	}

	printer := NewPrinter(opts...)
	newStderr := NewFileCapture(out)

	return &PrinterWithStdCapture{
		Printer:     printer,
		FileCapture: *newStderr,
	}
}
