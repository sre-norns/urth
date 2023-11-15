package runner

import (
	"bytes"
	"context"

	"github.com/sre-norns/urth/pkg/urth"
)

func runHarScript(ctx context.Context, scriptContent []byte, options RunOptions) (urth.FinalRunResults, error) {
	texLogger := RunLog{}

	texLogger.Log("replaying HAR file")

	harLog, err := UnmarshalHAR(bytes.NewReader(scriptContent))
	if err != nil {
		texLogger.Log("...failed to deserialize HAR file: ", err)
		return NewRunResultsWithLog(urth.RunFinishedError, &texLogger), err
	}

	requests, err := ConvertHarToHttpTester(harLog.Log.Entries)
	if err != nil {
		texLogger.Log("...failed to convert HAR file requests: ", err)
		return NewRunResultsWithLog(urth.RunFinishedError, &texLogger), err
	}

	return runHttpRequests(ctx, &texLogger, requests, options)
}
