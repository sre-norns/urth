package httpparser

import (
	"fmt"
	"io"
	"net/textproto"
	"strings"
)

func (r *TestRequest) Marshal(w io.Writer) error {
	fmt.Fprintf(w, "%v %v %v\n", r.Method, r.URL.Path, r.Proto)
	fmt.Fprintf(w, "%v: %v\n", textproto.CanonicalMIMEHeaderKey("Host"), r.URL.Host)

	for header, value := range r.Header {
		if header != textproto.CanonicalMIMEHeaderKey("Host") {
			fmt.Fprintf(w, "%v: %v\n", header, strings.Join(value, "; "))
		}
	}
	fmt.Fprint(w, "\r\n")
	// TODO: Format body

	return nil
}

func Marshal(w io.Writer, entries []TestRequest) error {
	for i, entry := range entries {
		if err := entry.Marshal(w); err != nil {
			return fmt.Errorf("failed to marshal TestRequest %d out of %d: %w", i+1, len(entries), err)
		}

		if i+1 != len(entries) {
			fmt.Fprint(w, "###\r\n")
		}
	}

	return nil
}
