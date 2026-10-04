package urth

import (
	"encoding/json"
	"github.com/sre-norns/urth/pkg/prob"
	"github.com/sre-norns/wyrd/pkg/manifest"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
	"testing"
	"time"
)

func TestVersionDomainOrdering(t *testing.T) {
	for _, tt := range []struct {
		raw, rangeText string
		match, invalid bool
	}{
		{"1.10.0", ">=1.9.0", true, false}, {"1.9.0", ">=1.10.0", false, false},
		{"1.10.0-alpha.1", ">=1.10.0", false, false}, {"1.10.0-alpha.2", ">1.10.0-alpha.1", true, false},
		{"unknown", ">=1.0.0", false, false}, {"(devel)", ">=1.0.0", false, false},
		{"1.2", ">=1.0.0", false, false}, {"1.2.0+dirty", ">=1.0.0 <2.0.0", true, false},
		{"1.2.0", "^1.0.0", false, true}, {"1.2.0", ">=bogus", false, true},
		{"1.2.0", ">=2.0.0 <1.0.0", false, true},
		{"1.2.0", ">2.0.0 <=2.0.0", false, true},
		{"1.2.0", ">=2.0.0 invalid", false, true},
	} {
		t.Run(tt.raw+tt.rangeText, func(t *testing.T) {
			ok, err := versionMatches(tt.raw, tt.rangeText)
			require.Equal(t, tt.invalid, err != nil)
			require.Equal(t, tt.match, ok)
		})
	}
}
func TestChannelCoverageAndDuration(t *testing.T) {
	p := RunnerSpec{JobRequirements: JobRequirements{ProbeKinds: []string{"http"}, MinDuration: "1s", MaxDuration: "10s"}}
	c := WorkerCapabilities{OS: "linux", Architecture: "amd64", ProbeVersions: map[string]string{"http": "unknown"}, MinDuration: "1ns", MaxDuration: "10s"}
	require.NoError(t, p.AdmitCapabilities(c, nil))
	c.MaxDuration = "9s"
	require.Error(t, p.AdmitCapabilities(c, nil))
	c.MaxDuration = "10s"
	c.MinDuration = "2s"
	require.Error(t, p.AdmitCapabilities(c, nil))
	c.MinDuration = "1ns"
	delete(c.ProbeVersions, "http")
	require.Error(t, p.AdmitCapabilities(c, nil))
	c.ProbeVersions["http"] = "1.10.0"
	p.WorkerRequirements.ProbeVersions = map[string]string{"http": ">=1.9.0"}
	require.NoError(t, p.AdmitCapabilities(c, nil))
	p.WorkerRequirements.ProbeVersions["http"] = ">=2.0.0"
	require.Error(t, p.AdmitCapabilities(c, nil))
	p.WorkerRequirements = WorkerRequirements{MaxDuration: "9s"}
	require.Error(t, p.ValidatePolicy())
	p.WorkerRequirements = WorkerRequirements{}
	for _, d := range []time.Duration{time.Second, 10 * time.Second} {
		require.NoError(t, p.AcceptsJob(ExecutionSnapshot{Prob: prob.Manifest{Kind: "http", Timeout: d}}, nil))
	}
	for _, d := range []time.Duration{time.Second - time.Nanosecond, 10*time.Second + time.Nanosecond} {
		require.Error(t, p.AcceptsJob(ExecutionSnapshot{Prob: prob.Manifest{Kind: "http", Timeout: d}}, nil))
	}
	require.Error(t, (RunnerSpec{}).AcceptsJob(ExecutionSnapshot{Prob: prob.Manifest{Kind: "http"}}, nil))
	p.PropagatedLabels = manifest.Labels{LabelWorkerUID: "forged"}
	require.Error(t, p.ValidatePolicy())
}
func TestObsoleteRunnerPolicyRejected(t *testing.T) {
	var spec RunnerSpec
	require.ErrorContains(t, json.Unmarshal([]byte(`{"requirements":{}}`), &spec), "obsolete")
	require.ErrorContains(t, yaml.Unmarshal([]byte("requirements: {}\n"), &spec), "obsolete")
	require.Error(t, json.Unmarshal([]byte(`{"jobRequirements":{"probeKinds":["http"],"maxDuraton":"1s"}}`), &spec))
	require.ErrorContains(t, yaml.Unmarshal([]byte("jobRequirements:\n  probeKinds: [http]\n  maxDuraton: 1s\n"), &spec), "maxDuraton")
}
