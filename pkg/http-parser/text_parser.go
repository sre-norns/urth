package httpparser

import (
	"bytes"
	"io"
	"strings"
)

// commentFilteringReader wraps a reader over a script, stripping `#` comments
// before the content reaches the request parser.
//
// Filtering is line oriented, so the whole of the underlying reader is consumed
// on the first Read: a comment marker can otherwise straddle the boundary
// between two reads, and filtering a chunk at a time would let a `#` in one
// chunk hide text that was already emitted in the previous one.
type commentFilteringReader struct {
	reader io.Reader

	filtered *bytes.Reader
	readErr  error
}

// filterComments removes comments from a script:
//   - a `#` starts a comment that runs to the end of the line,
//   - a line that holds nothing but a comment is removed entirely,
//   - a line that was already blank is preserved, since blank lines separate
//     requests,
//   - trailing blank lines are dropped.
func filterComments(content []byte) []byte {
	lines := strings.Split(string(content), "\n")
	kept := make([]string, 0, len(lines))

	for _, line := range lines {
		text, _, hadComment := strings.Cut(line, "#")
		text = strings.TrimSpace(text)

		// A comment-only line is dropped, rather than left behind as a blank.
		if text == "" && hadComment {
			continue
		}

		kept = append(kept, text)
	}

	return []byte(strings.TrimRight(strings.Join(kept, "\n"), "\n"))
}

func (r *commentFilteringReader) load() error {
	if r.filtered != nil || r.readErr != nil {
		return r.readErr
	}

	content, err := io.ReadAll(r.reader)
	if err != nil {
		r.readErr = err
		return err
	}

	r.filtered = bytes.NewReader(filterComments(content))

	return nil
}

func (r *commentFilteringReader) Read(p []byte) (n int, err error) {
	if err := r.load(); err != nil {
		return 0, err
	}

	return r.filtered.Read(p)
}
