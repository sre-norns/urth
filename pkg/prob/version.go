package prob

import (
	"runtime/debug"
	"strings"
)

// buildVersion is stamped by release builds that cannot embed VCS metadata.
// The container images build from a context without .git, so Go build info
// reports "(devel)" there; without this override every image Worker would
// declare a version that no channel version range can accept.
var buildVersion string

// BuildVersion reports the version of this binary: the stamped release version
// when one was supplied, otherwise the main module version from Go build info.
// The result is raw: "devel" and pseudo-versions are reported as they are, and
// channel policy decides whether they satisfy a range.
func BuildVersion() string {
	if v := strings.TrimSpace(buildVersion); v != "" {
		return v
	}
	if bi, ok := debug.ReadBuildInfo(); ok {
		if v := strings.Trim(bi.Main.Version, "() "); v != "" {
			return v
		}
	}
	return "devel"
}
