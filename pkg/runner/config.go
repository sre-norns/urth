package runner

import (
	"log"
	"os/exec"
	"runtime"
	"runtime/debug"
	"strings"
	"time"

	// TODO: move to github.com/sre-norns/wyrd
	"github.com/sre-norns/urth/pkg/wyrd"
)

const (
	LabelOS            = "runner.os"
	LabelArch          = "runner.arch"
	LabelNodeJsVersion = "runner.node.version"
	LabelPythonVersion = "runner.python.version"

	// Well-known labels used by runners:
	LabelBuildVersion      = "runner.version"
	LabelRunnerId          = "runner.id"
	LabelRunnerVersionedId = "runner.id.versioned"
)

type RunnerConfig struct {
	systemLabels wyrd.Labels        `kong:"-"`
	CustomLabels wyrd.Labels        `help:"Extra labels to identify this instance of the runner"`
	Requirements wyrd.LabelSelector `kong:"-"`

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

	return wyrd.Labels{
		LabelNodeJsVersion: strings.TrimSpace(string(out)),
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

	return wyrd.Labels{
		LabelPythonVersion: strings.TrimSpace(parts[1]),
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
		),
	}
}
