// Package content decides whether a file holds text.
//
// The test is the one git uses: a NUL byte in the first few thousand
// bytes means binary. It is a heuristic, but a dependable one for what
// over needs it for -- telling a script in a directory of them from a
// compiled program sitting alongside.
package content

import (
	"bytes"
	"io"
	"os"
)

// sniff is how much of a file is examined. Executables put a NUL in the
// first handful of bytes, so this is generous.
const sniff = 8000

// IsBinary reports whether data looks like binary content.
func IsBinary(data []byte) bool {
	if len(data) > sniff {
		data = data[:sniff]
	}
	return bytes.IndexByte(data, 0) >= 0
}

// IsBinaryFile reports whether the file at path looks like binary
// content. Only the head of the file is read. A file that cannot be read
// as a regular file is not text, and says so through the error.
func IsBinaryFile(path string) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()
	buf := make([]byte, sniff)
	n, err := io.ReadFull(f, buf)
	switch {
	case err == nil || err == io.ErrUnexpectedEOF || err == io.EOF:
	default:
		return false, err
	}
	return IsBinary(buf[:n]), nil
}
