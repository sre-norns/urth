package har

import (
	"bytes"
	"context"
	"fmt"
	"reflect"
	"runtime/debug"
	"strings"

	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/sre-norns/urth/pkg/prob"
	"github.com/sre-norns/urth/pkg/probers/rest"
	"github.com/sre-norns/wyrd/pkg/manifest"
)

const (
	Kind           = prob.Kind("har")
	ScriptMimeType = "application/json"
)

type Spec struct {
	FollowRedirects bool   `json:"followRedirects,omitempty" yaml:"followRedirects,omitempty"`
	Script          string `json:"script,omitempty" yaml:"script,omitempty"`
}

func init() {
	moduleVersion := "devel"
	if bi, ok := debug.ReadBuildInfo(); ok {
		moduleVersion = strings.Trim(bi.Main.Version, "()")
	}

	// Ignore double registration error
	_ = prob.RegisterProbKind(
		Kind,
		&Spec{},
		prob.ProbRegistration{
			RunFunc:     RunScript,
			ContentType: ScriptMimeType,
			Version:     moduleVersion,
		})
}

func RunScript(ctx context.Context, probSpec any, config prob.RunOptions, registry *prometheus.Registry, logger log.Logger) (prob.RunStatus, []prob.Artifact, error) {
	spec, ok := probSpec.(*Spec)
	if !ok {
		return prob.RunFinishedError, nil, fmt.Errorf("%w: got %q, expected %q", manifest.ErrUnexpectedSpecType, reflect.TypeOf(probSpec), reflect.TypeOf(&Spec{}))
	}

	logger.Log("replaying HAR file")
	harLog, err := UnmarshalHAR(bytes.NewReader([]byte(spec.Script)))
	if err != nil {
		logger.Log("...failed to deserialize HAR file: ", err)
		return prob.RunFinishedError, nil, nil
	}

	requests, err := ConvertHarToHttpTester(harLog.Log.Entries)
	if err != nil {
		logger.Log("...failed to convert HAR file requests: ", err)
		return prob.RunFinishedError, nil, err
	}

	return rest.RunHttpRequests(ctx, requests, config, logger)
}
