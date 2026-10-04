package urth_test

import "github.com/sre-norns/urth/pkg/urth"

func testPolicyCapabilities() urth.WorkerCapabilities {
	versions := map[string]string{}
	for _, kind := range []string{"http", "tcp", "rest", "dns", "grpc", "icmp", "har", "puppeteer"} {
		versions[kind] = "1.10.0"
	}
	return urth.WorkerCapabilities{Privileges: []string{"raw-sockets"}, Version: "1.10.0", OS: "linux", Architecture: "amd64", ProbeVersions: versions, MinDuration: "1ns", MaxDuration: "1h"}
}
