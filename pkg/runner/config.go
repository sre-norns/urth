package runner

import (
	"os/exec"
	"runtime"
	"time"

	// TODO: move to github.com/sre-norns/wyrd
	"github.com/sre-norns/urth/pkg/wyrd"
)

const (
	LabelOS     = "os"
	LabelArch   = "arch"
	LabelNode   = "node"
	LabelPython = "python"
)

type RunnerConfig struct {
	SystemLabels wyrd.Labels        `kong:"-"`
	Labels       wyrd.Labels        `help:"Extra labels to identify this instance of the runner"`
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
		LabelNode: string(out),
	}
}

func GetPythonRuntimeLabels() wyrd.Labels {
	nodeV := exec.Command("python", "-V")
	out, err := nodeV.CombinedOutput()
	if err != nil {
		return wyrd.Labels{}
	}

	return wyrd.Labels{
		LabelPython: string(out),
	}
}

func GetRuntimeLabels() wyrd.Labels {
	return wyrd.Labels{
		LabelArch: runtime.GOARCH,
		LabelOS:   runtime.GOOS,
	}
}

func NewDefaultConfig() RunnerConfig {
	labels := wyrd.MergeLabels(
		GetRuntimeLabels(),
		GetNodeRuntimeLabels(),
		GetPythonRuntimeLabels(),
	)

	return RunnerConfig{
		SystemLabels: labels,
	}
}
