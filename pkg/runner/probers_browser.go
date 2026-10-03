//go:build !urth_native

package runner

// The native Worker image has no Node or browser runtime. Keep its advertised
// probe registry consistent with its executable runtime profile.
import _ "github.com/sre-norns/urth/pkg/probers/puppeteer"
