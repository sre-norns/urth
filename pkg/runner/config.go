package runner

import (
	"fmt"
	"log"
	"os/exec"
	"runtime"
	"runtime/debug"
	"strings"
	"time"

	// TODO: move to github.com/sre-norns/wyrd
	"github.com/sre-norns/urth/pkg/urth"
	"github.com/sre-norns/urth/pkg/wyrd"
	"golang.org/x/mod/semver"
)

const (
	LabelOS   = "runner.os"
	LabelArch = "runner.arch"

	// Runtimes available:
	LabelNodeJsVersion      = "runner.node.version"
	LabelNodeJsVersionMajor = "runner.node.version" + ".major"

	LabelPythonVersion      = "runner.python.version"
	LabelPythonVersionMajor = LabelPythonVersion + ".major"

	// Well-known labels used by runners:
	LabelBuildVersion      = "runner.version"
	LabelRunnerId          = "runner.id"
	LabelRunnerVersionedId = "runner.id.versioned"
)

type RunnerConfig struct {
	systemLabels wyrd.Labels `kong:"-"`
	CustomLabels wyrd.Labels `help:"Extra labels to identify this instance of the runner"`

	ApiToken         string        `help:"API token to register this runner instance"`
	ApiServerAddress string        `help:"URL address of the API server" default:"http://localhost:8080/" `
	WorkingDirectory string        `help:"Worker directory where test are executed" default:"./worker" type:"existingdir"`
	Timeout          time.Duration `help:"Maximum duration alloted for each script run" default:"1m"`
}

func GetNodeRuntimeLabels() wyrd.Labels {
	nodeV := exec.Command("node", "-v")
	out, err := nodeV.CombinedOutput()
	if err != nil {
		return wyrd.Labels{}
	}

	vstr := strings.TrimSpace(string(out))
	return wyrd.Labels{
		LabelNodeJsVersion:      vstr[1:],
		LabelNodeJsVersionMajor: semver.Major(vstr)[1:],
	}
}

func GetPythonRuntimeLabels() wyrd.Labels {
	nodeV := exec.Command("python3", "-V")
	out, err := nodeV.CombinedOutput()
	if err != nil {
		return wyrd.Labels{}
	}

	parts := strings.Split(strings.TrimSpace(string(out)), " ")
	if len(parts) < 2 {
		return wyrd.Labels{}
	}

	vstr := strings.TrimSpace(parts[1])
	return wyrd.Labels{
		LabelPythonVersion:      vstr,
		LabelPythonVersionMajor: semver.Major("v" + vstr)[1:],
	}
}

func GetRuntimeLabels() wyrd.Labels {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		log.Print("[ERROR] failed to get Build info")
	}

	return wyrd.Labels{
		LabelArch:         runtime.GOARCH,
		LabelOS:           runtime.GOOS,
		LabelBuildVersion: bi.Main.Version,
	}
}

func (c *RunnerConfig) GetEffectiveLabels() wyrd.Labels {
	return wyrd.MergeLabels(
		c.systemLabels,
		c.CustomLabels,
	)
}

func NewDefaultConfig() RunnerConfig {
	return RunnerConfig{
		systemLabels: wyrd.MergeLabels(
			GetRuntimeLabels(),
			GetNodeRuntimeLabels(),
			GetPythonRuntimeLabels(),
			ProberAsLabels(),
		),
	}
}

func kindAsLabel(kind urth.ProbKind) string {
	return fmt.Sprintf("runner.prob.%v", kind)
}

// Expose loaded probers as Labels
func ProberAsLabels() wyrd.Labels {
	probs := ListProbs()
	result := make(wyrd.Labels, len(probs))
	for kind, prob := range probs {
		result[kindAsLabel(kind)] = prob.Version
	}

	return result
}

func (c *RunnerConfig) LabelJob(runnerId urth.VersionedResourceId, job urth.RunScenarioJob) wyrd.Labels {

	return wyrd.MergeLabels(
		job.Labels,
		c.GetEffectiveLabels(),
		wyrd.Labels{
			LabelRunnerId:          runnerId.ID.String(), // Groups all artifacts produced by the same runner
			LabelRunnerVersionedId: runnerId.String(),    // Groups all artifacts produced by the same version of the scenario

			urth.LabelScenarioId:          job.ScenarioID.ID.String(), // Groups all artifacts produced by the same scenario regardless of version
			urth.LabelScenarioVersionedId: job.ScenarioID.String(),    // Groups all artifacts produced by the same version of the scenario
			urth.LabelScenarioKind:        string(job.Prob.Kind),      // Groups all artifacts produced by the type of script: TCP probe, HTTP probe, etc.
		},
	)
}
