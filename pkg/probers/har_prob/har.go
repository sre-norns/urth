package har_prob

import (
	"bytes"
	"context"
	"fmt"
	"runtime/debug"

	"github.com/sre-norns/urth/pkg/probers/http_prob"
	"github.com/sre-norns/urth/pkg/runner"
	"github.com/sre-norns/urth/pkg/urth"
)

const (
	Kind           = urth.ProbKind("har")
	ScriptMimeType = "application/json"
)

type Spec struct {
	FollowRedirects bool
	Script          string
}

func init() {
	moduleVersion := "(unknown)"
	bi, ok := debug.ReadBuildInfo()
	if ok {
		moduleVersion = bi.Main.Version
	}

	// Ignore double registration error
	_ = runner.RegisterProbKind(
		Kind,
		&Spec{},
		runner.ProbRegistration{
			RunFunc:     RunScript,
			ContentType: ScriptMimeType,
			Version:     moduleVersion,
		})
}

func RunScript(ctx context.Context, probSpec any, options runner.RunOptions) (urth.FinalRunResults, []urth.ArtifactSpec, error) {
	texLogger := runner.RunLog{}
	prob, ok := probSpec.(*Spec)
	if !ok {
		return urth.NewRunResults(urth.RunFinishedError), texLogger.Package(), fmt.Errorf("invalid spec")
	}
	texLogger.Log("replaying HAR file")

	harLog, err := UnmarshalHAR(bytes.NewReader([]byte(prob.Script)))
	if err != nil {
		texLogger.Log("...failed to deserialize HAR file: ", err)
		return urth.NewRunResults(urth.RunFinishedError), texLogger.Package(), nil
	}

	requests, err := ConvertHarToHttpTester(harLog.Log.Entries)
	if err != nil {
		texLogger.Log("...failed to convert HAR file requests: ", err)
		return urth.NewRunResults(urth.RunFinishedError), texLogger.Package(), err
	}

	return http_prob.RunHttpRequests(ctx, &texLogger, requests, options)
}
