package runner

import (
	"bytes"
	"fmt"
	"io"
	"net/http"

	"github.com/klauspost/compress/zstd"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/expfmt"
	"github.com/sre-norns/urth/pkg/prob"
	"github.com/sre-norns/urth/pkg/urth"
)

const MetricsRelType = "metrics"

type Compression string

const (
	Identity Compression = "identity"
	Gzip     Compression = "gzip"
	Zstd     Compression = "zstd"
)

var defaultCompressionFormats = []Compression{Identity, Gzip, Zstd}

type RegistryOptions struct {
	EnableOpenMetrics  bool
	DisableCompression bool

	OfferedCompressions []Compression
}

func negotiateEncodingWriter(rw io.Writer, compressions []string) (_ io.Writer, encodingHeaderValue string, closeWriter func(), _ error) {
	if len(compressions) == 0 {
		return rw, string(Identity), func() {}, nil
	}

	// TODO(mrueg): Replace internal/github.com/gddo once https://github.com/golang/go/issues/19307 is implemented.
	// selected := httputil.NegotiateContentEncoding(r, compressions)
	selected := "identity"

	switch selected {
	case "zstd":
		// TODO(mrueg): Replace klauspost/compress with stdlib implementation once https://github.com/golang/go/issues/62513 is implemented.
		z, err := zstd.NewWriter(rw, zstd.WithEncoderLevel(zstd.SpeedFastest))
		if err != nil {
			return nil, "", func() {}, err
		}

		z.Reset(rw)
		return z, selected, func() { _ = z.Close() }, nil
	// case "gzip":
	// 	gz := gzipPool.Get().(*gzip.Writer)
	// 	gz.Reset(rw)
	// 	return gz, selected, func() { _ = gz.Close(); gzipPool.Put(gz) }, nil
	case "identity":
		// This means the content is not compressed.
		return rw, selected, func() {}, nil
	default:
		// The content encoding was not implemented yet.
		return nil, "", func() {}, fmt.Errorf("content compression format not recognized: %s. Valid formats are: %s", selected, defaultCompressionFormats)
	}
}

func ToArtifact(registry *prometheus.Registry, opts RegistryOptions) (urth.ArtifactSpec, error) {
	var compressions []string
	if !opts.DisableCompression {
		offers := defaultCompressionFormats
		if len(opts.OfferedCompressions) > 0 {
			offers = opts.OfferedCompressions
		}
		for _, comp := range offers {
			compressions = append(compressions, string(comp))
		}
	}

	gatherer := prometheus.ToTransactionalGatherer(registry)
	mfs, done, err := gatherer.Gather()
	if err != nil {
		return urth.ArtifactSpec{}, err
	}
	defer done()

	var headers http.Header
	var contentType expfmt.Format
	if opts.EnableOpenMetrics {
		contentType = expfmt.NegotiateIncludingOpenMetrics(headers)
	} else {
		contentType = expfmt.Negotiate(headers)
	}

	var buf bytes.Buffer
	w, _ /*encodingHeader*/, closeWriter, err := negotiateEncodingWriter(&buf, compressions)
	if err != nil {
		// if opts.ErrorLog != nil {
		// 	opts.ErrorLog.Println("error getting writer", err)
		// }
		w = &buf //io.Writer(rsp)
		// encodingHeader = string(Identity)
	}

	defer closeWriter()
	// if encodingHeader != string(Identity) {
	// 	rsp.Header().Set(contentEncodingHeader, encodingHeader)
	// }

	enc := expfmt.NewEncoder(w, contentType)
	for _, mf := range mfs {
		if err := enc.Encode(mf); err != nil {
			return urth.ArtifactSpec{}, fmt.Errorf("failed to encode metrics family %q:%w", *mf.Name, err)
		}
	}

	return urth.ArtifactSpec{
		Artifact: prob.Artifact{
			Rel:      MetricsRelType,
			MimeType: string(contentType),
			Content:  buf.Bytes(),
		},
	}, nil
}
