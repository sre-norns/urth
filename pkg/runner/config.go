package runner

import (
	"fmt"
	"log"
	"os/exec"
	"runtime"
	"runtime/debug"
	"strings"
	"time"

	"github.com/sre-norns/urth/pkg/urth"
	"github.com/sre-norns/wyrd/pkg/manifest"
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
	LabelBuildVersion  = "runner.version"
	LabelRunnerName    = "runner.name"
	LabelRunnerUID     = "runner.uid"
	LabelRunnerVersion = "runner.version"
)

type RunnerConfig struct {
	systemLabels manifest.Labels `kong:"-"`
	CustomLabels manifest.Labels `help:"Extra labels to identify this instance of the runner"`

	WorkingDirectory string        `help:"Worker directory where test are executed" default:"./worker" type:"existingdir"`
	Timeout          time.Duration `help:"Maximum duration alloted for each script run" default:"1m"`
}

func GetNodeRuntimeLabels() manifest.Labels {
	nodeV := exec.Command("node", "-v")
	out, err := nodeV.CombinedOutput()
	if err != nil {
		return manifest.Labels{}
	}

	vstr := strings.TrimSpace(string(out))
	return manifest.Labels{
		LabelNodeJsVersion:      vstr[1:],
		LabelNodeJsVersionMajor: semver.Major(vstr)[1:],
	}
}

func GetPythonRuntimeLabels() manifest.Labels {
	nodeV := exec.Command("python3", "-V")
	out, err := nodeV.CombinedOutput()
	if err != nil {
		return manifest.Labels{}
	}

	parts := strings.Split(strings.TrimSpace(string(out)), " ")
	if len(parts) < 2 {
		return manifest.Labels{}
	}

	vstr := strings.TrimSpace(parts[1])
	return manifest.Labels{
		LabelPythonVersion:      vstr,
		LabelPythonVersionMajor: semver.Major("v" + vstr)[1:],
	}
}

func GetRuntimeLabels() manifest.Labels {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		log.Print("[ERROR] failed to get Build info")
	}

	bi.Main.Version = strings.Trim(bi.Main.Version, "()")
	return manifest.Labels{
		LabelArch:         runtime.GOARCH,
		LabelOS:           runtime.GOOS,
		LabelBuildVersion: bi.Main.Version,
	}
}

func (c *RunnerConfig) GetEffectiveLabels() manifest.Labels {
	return manifest.MergeLabels(
		c.systemLabels,
		c.CustomLabels,
	)
}

func NewDefaultConfig() RunnerConfig {
	return RunnerConfig{
		systemLabels: manifest.MergeLabels(
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
func ProberAsLabels() manifest.Labels {
	probs := ListProbs()
	result := make(manifest.Labels, len(probs))
	for kind, prob := range probs {
		result[kindAsLabel(kind)] = prob.Version
	}

	return result
}

func (c *RunnerConfig) LabelJob(runnerName manifest.ResourceName, runnerId manifest.VersionedResourceID, job urth.Job) manifest.Labels {
	return manifest.MergeLabels(
		job.Labels,
		c.GetEffectiveLabels(),
		manifest.Labels{
			LabelRunnerName:    string(runnerName),        // Groups all artifacts produced by the same runner
			LabelRunnerUID:     string(runnerId.ID),       // Groups all artifacts produced by the same runner
			LabelRunnerVersion: runnerId.Version.String(), // Groups all artifacts produced by the same runner

			urth.LabelRunResultsName: string(job.ResultName),   // Groups all artifacts produced in the same run
			urth.LabelScenarioId:     string(job.ScenarioName), // Groups all artifacts produced by the same scenario regardless of version
			// urth.LabelScenarioVersion: job.ScenarioID.String(),  // Groups all artifacts produced by the same version of the scenario
			urth.LabelScenarioKind: string(job.Prob.Kind), // Groups all artifacts produced by the type of script: TCP probe, HTTP probe, etc.
		},
	)
}
