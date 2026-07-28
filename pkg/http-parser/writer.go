package httpparser

import (
	"fmt"
	"io"
	"net/textproto"
	"strings"
)

const defaultProtoVersion = "HTTP/1.1"

// bodyContent returns the request's body without consuming it, where the
// request can say how to read it twice.
func (r *TestRequest) bodyContent() ([]byte, error) {
	switch {
	case r.GetBody != nil:
		body, err := r.GetBody()
		if err != nil {
			return nil, err
		}
		defer body.Close()

		return io.ReadAll(body)

	case r.Body != nil:
		// No GetBody: the body can only be read once, so reading it here spends
		// it. That is the right trade for a converter, which writes the request
		// out rather than sending it.
		defer r.Body.Close()

		return io.ReadAll(r.Body)
	}

	return nil, nil
}

// Marshal writes the request as a `.http` script entry.
func (r *TestRequest) Marshal(w io.Writer) error {
	if r.Name != "" {
		fmt.Fprintf(w, "# @name %v\n", r.Name)
	}

	protoVersion := r.Proto
	if protoVersion == "" {
		protoVersion = defaultProtoVersion
	}

	// The absolute form is used rather than a path plus a `Host` header, since
	// the path form has nowhere to put the scheme: `GET /x` + `Host: go.dev`
	// re-reads as https, and a plain-http request would not survive the trip.
	fmt.Fprintf(w, "%v %v %v\n", r.Method, r.URL, protoVersion)

	// Host is a field on the request, not an entry in Header, and is only worth
	// writing when it differs from the host being dialled.
	if r.Host != "" && r.Host != r.URL.Host {
		fmt.Fprintf(w, "%v: %v\n", textproto.CanonicalMIMEHeaderKey("Host"), r.Host)
	}

	for header, value := range r.Header {
		if header != textproto.CanonicalMIMEHeaderKey("Host") {
			fmt.Fprintf(w, "%v: %v\n", header, strings.Join(value, "; "))
		}
	}

	body, err := r.bodyContent()
	if err != nil {
		return fmt.Errorf("failed to read request body: %w", err)
	}

	if len(body) > 0 {
		// Exactly one blank line separates the headers from the body.
		fmt.Fprintf(w, "\n%s\n", body)
	}

	return nil
}

func Marshal(w io.Writer, entries []TestRequest) error {
	for i, entry := range entries {
		if err := entry.Marshal(w); err != nil {
			return fmt.Errorf("failed to marshal TestRequest %d out of %d: %w", i+1, len(entries), err)
		}

		if i+1 != len(entries) {
			fmt.Fprintf(w, "%v\n", requestSeparator)
		}
	}

	return nil
}
