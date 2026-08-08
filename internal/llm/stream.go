package llm

import (
	"bufio"
	"io"
)

// lineScanner reads lines from a stream, handling both \n and \r\n.
type lineScanner struct {
	scanner *bufio.Scanner
}

func newLineScanner(r io.Reader) *lineScanner {
	return &lineScanner{scanner: bufio.NewScanner(r)}
}

func (s *lineScanner) ReadLine() (string, error) {
	if !s.scanner.Scan() {
		if err := s.scanner.Err(); err != nil {
			return "", err
		}
		return "", io.EOF
	}
	return s.scanner.Text(), nil
}
