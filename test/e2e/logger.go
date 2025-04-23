package e2e

import (
	"bytes"
	"io"
	"os"
	"sync"

	"github.com/go-logr/logr"
	"github.com/go-logr/zapr"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

type LoggerConfig struct {
	NoColor bool
	syncer  zapcore.WriteSyncer
}

func (c LoggerConfig) Apply(opts *LoggerConfig) {
	opts.NoColor = c.NoColor
	if c.syncer != nil {
		opts.syncer = c.syncer
	}
}

type LoggerOption interface {
	Apply(*LoggerConfig)
}

// WithOutputFile returns a LoggerOption that configures the logger to write to both
// the specified file and stdout.
func WithOutputFile(filename string) LoggerOption {
	return &withOutputFileOption{filename: filename}
}

type withOutputFileOption struct {
	filename string
}

func (o *withOutputFileOption) Apply(cfg *LoggerConfig) {
	file, err := os.OpenFile(o.filename, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		// Fall back to stdout only if file can't be opened
		cfg.syncer = zapcore.AddSync(os.Stdout)
		return
	}

	// Use MultiWriteSyncer to write to both stdout and the file
	multiSyncer := zapcore.NewMultiWriteSyncer(
		zapcore.AddSync(os.Stdout),
		zapcore.AddSync(file),
	)
	cfg.syncer = multiSyncer
}

func NewLogger(opts ...LoggerOption) logr.Logger {
	cfg := &LoggerConfig{}
	for _, opt := range opts {
		opt.Apply(cfg)
	}

	if cfg.syncer == nil {
		cfg.syncer = zapcore.AddSync(os.Stdout)
	}

	encoderCfg := zap.NewDevelopmentEncoderConfig()
	if !cfg.NoColor {
		encoderCfg.EncodeLevel = zapcore.CapitalColorLevelEncoder
	}

	consoleEncoder := zapcore.NewConsoleEncoder(encoderCfg)
	core := zapcore.NewCore(consoleEncoder, cfg.syncer, zapcore.DebugLevel)
	log := zap.New(core)

	return zapr.NewLogger(log)
}

// NewPausableLogger returns a logger that can be paused and resumed.
func NewPausableLogger(opts ...LoggerOption) PausableLogger {
	cfg := &LoggerConfig{}
	for _, opt := range opts {
		opt.Apply(cfg)
	}

	var activeSyncer zapcore.WriteSyncer
	if cfg.syncer != nil {
		activeSyncer = cfg.syncer
	} else {
		activeSyncer = zapcore.AddSync(os.Stdout)
	}
	syncer := newSwitchSyncer(activeSyncer)
	cfg.syncer = syncer

	return PausableLogger{
		Logger: NewLogger(cfg),
		syncer: syncer,
	}
}

// WithSwitchWriter returns a LoggerOption that sets the output writer to the given SwitchWriter.
func WithSwitchWriter(w *SwitchWriter) LoggerOption {
	return &withSwitchWriterOption{writer: w}
}

type withSwitchWriterOption struct {
	writer io.Writer
}

func (o *withSwitchWriterOption) Apply(cfg *LoggerConfig) {
	cfg.syncer = zapcore.AddSync(o.writer)
}

// PausableLogger can be paused and resumed. It wraps a logr.Logger.
// When paused, writes go to a buffer; when resumed, writes go to stdout.
// After it's resumed, the buffer is flushed to stdout.
type PausableLogger struct {
	logr.Logger
	syncer *switchSyncer
}

func (l PausableLogger) Pause() {
	l.syncer.Pause()
}

func (l PausableLogger) Resume() error {
	return l.syncer.Resume()
}

// switchSyncer implements zapcore.WriteSyncer.
// When paused, writes go to a buffer; when resumed, writes go to the active writer.
type switchSyncer struct {
	*SwitchWriter
	active zapcore.WriteSyncer // normally stdout
}

func newSwitchSyncer(active zapcore.WriteSyncer) *switchSyncer {
	return &switchSyncer{
		active:       active,
		SwitchWriter: NewSwitchWriter(active),
	}
}

func (s *switchSyncer) Sync() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.paused {
		return nil
	}
	return s.active.Sync()
}

// SwitchWriter implements io.Writer.
// When paused, writes go to a buffer; when resumed, writes go to the active writer.
type SwitchWriter struct {
	mu     sync.Mutex
	active io.Writer     // actual writer where we want to output
	buffer *bytes.Buffer // holds bytes while paused
	paused bool
}

var _ io.Writer = &SwitchWriter{}

func NewSwitchWriter(active io.Writer) *SwitchWriter {
	return &SwitchWriter{
		active: active,
		buffer: &bytes.Buffer{},
	}
}

func (s *SwitchWriter) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.paused {
		return s.buffer.Write(p)
	}
	return s.active.Write(p)
}

// Pause causes subsequent writes to be buffered.
func (s *SwitchWriter) Pause() {
	s.mu.Lock()
	s.paused = true
	s.mu.Unlock()
}

// Resume flushes the buffer to the active writer and resumes normal output.
func (s *SwitchWriter) Resume() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.buffer.Len() > 0 {
		if _, err := s.active.Write(s.buffer.Bytes()); err != nil {
			return err
		}
		s.buffer.Reset()
	}
	s.paused = false
	return nil
}
