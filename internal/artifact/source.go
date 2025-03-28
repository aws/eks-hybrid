package artifact

import (
	"fmt"
	"hash"
	"io"
)

// Source is a binary data source. Sources must be streamed in their entirety before their
// checksums can be validated. Sources are closable. It is up to the producer to determine who
// is responsible for closing the Source.
type Source interface {
	io.ReadCloser
	ChecksumVerifier
}

// ChecksumVerifier verifies the checksum of a data source.
type ChecksumVerifier interface {
	// VerifyChecksum returns true if ExpectedChecksum() is equal to ActualChecksum(). Otherwise
	// it returns false. If expected and actual are nil, it returns true.
	VerifyChecksum() bool

	// ExpectedChecksum returns the expected checksum of the underlying data.
	ExpectedChecksum() []byte

	// ActualChecksum returns the actual checksum of underlying data.
	ActualChecksum() []byte
}

// WithChecksum creates a checksumVerifier. The digest is used to calculate srcs checksum.
// The returned Source should be used to read the artifact contents.
func WithChecksum(rc io.ReadCloser, digest hash.Hash, expect []byte) (Source, error) {
	parsedExpectedChecksum, err := ParseGNUChecksum(expect)
	if err != nil {
		return nil, fmt.Errorf("parsing expected checksum: %w", err)
	}
	return struct {
		io.Reader
		io.Closer
		ChecksumVerifier
	}{
		Reader:           io.TeeReader(rc, digest),
		Closer:           rc,
		ChecksumVerifier: checksumVerifier{expect: parsedExpectedChecksum, digest: digest},
	}, nil
}

// WithNopChecksum turns rc into a Source that nops when the ChecksumVerifier methods are called.
// Both ActualChecksum() and ExpectedChecksum() will return nil.
func WithNopChecksum(rc io.ReadCloser) Source {
	return struct {
		io.ReadCloser
		ChecksumVerifier
	}{
		ReadCloser:       rc,
		ChecksumVerifier: nopChecksumVerifier{},
	}
}
