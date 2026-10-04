package integration

import "github.com/sre-norns/urth/pkg/urth"

func testPolicyCapabilities() urth.WorkerCapabilities {
	versions := map[string]string{}
	for _, kind := range []string{string(testProbKind), "http", "tcp"} {
		versions[kind] = "1.10.0"
	}
	return urth.WorkerCapabilities{Version: "1.10.0", OS: "linux", Architecture: "amd64", ProbeVersions: versions, MinDuration: "1ns", MaxDuration: "1h"}
}
