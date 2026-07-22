package runner

import (
	"os"
	"os/user"
	"strings"

	"github.com/sre-norns/urth/pkg/urth"
	"github.com/sre-norns/wyrd/pkg/manifest"
)

// GenerateWorkerName derives a default name for a worker process.
//
// A name built from the user and host is worth the trouble because it is what
// an operator reads when deciding which worker is misbehaving: "alice.build-7"
// locates a machine, a random token does not.
//
// Every component is validated before use and the whole thing falls back to a
// random token, because resource names are subdomain-shaped and a worker whose
// hostname contains an underscore -- or is a bare IP address -- would otherwise
// fail to register at all, which is a confusing way to discover a naming rule.
func GenerateWorkerName() manifest.ResourceName {
	name := ""

	if uname, err := user.Current(); err == nil && manifest.ValidateSubdomainName(uname.Name) == nil {
		name = uname.Name
	}

	if hostname, err := os.Hostname(); err == nil && manifest.ValidateSubdomainName(hostname) == nil {
		if name != "" {
			name += "."
		}
		name += hostname
	}

	if manifest.ValidateSubdomainName(name) != nil {
		name = string(urth.NewRandToken(16))
	}

	return manifest.ResourceName(strings.ToLower(name))
}
