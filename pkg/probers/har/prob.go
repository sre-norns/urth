package har

import (
	"bytes"
	"context"
	"fmt"
	"reflect"
	"runtime/debug"
	"strings"

	"github.com/sre-norns/urth/pkg/probers/http"
	"github.com/sre-norns/urth/pkg/runner"
	"github.com/sre-norns/urth/pkg/urth"
	"github.com/sre-norns/urth/pkg/wyrd"
)

const (
	Kind           = urth.ProbKind("har")
	ScriptMimeType = "application/json"
)

type Spec struct {
	FollowRedirects bool   `json:"followRedirects,omitempty" yaml:"followRedirects,omitempty"`
	Script          string `json:"script,omitempty" yaml:"script,omitempty"`
}

func init() {
	moduleVersion := "unknown"
	if bi, ok := debug.ReadBuildInfo(); ok {
		moduleVersion = strings.Trim(bi.Main.Version, "()")
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

func RunScript(ctx context.Context, probSpec any, logger *runner.RunLog, options runner.RunOptions) (urth.ResultStatus, []urth.ArtifactSpec, error) {
	prob, ok := probSpec.(*Spec)
	if !ok {
		return urth.NewRunResults(urth.RunFinishedError), logger.Package(), fmt.Errorf("%w: got %q, expected %q", wyrd.ErrUnexpectedSpecType, reflect.TypeOf(probSpec), reflect.TypeOf(&Spec{}))
	}
	logger.Log("replaying HAR file")

	harLog, err := UnmarshalHAR(bytes.NewReader([]byte(prob.Script)))
	if err != nil {
		logger.Log("...failed to deserialize HAR file: ", err)
		return urth.NewRunResults(urth.RunFinishedError), logger.Package(), nil
	}

	requests, err := ConvertHarToHttpTester(harLog.Log.Entries)
	if err != nil {
		logger.Log("...failed to convert HAR file requests: ", err)
		return urth.NewRunResults(urth.RunFinishedError), logger.Package(), err
	}

	return http.RunHttpRequests(ctx, logger, requests, options)
}
