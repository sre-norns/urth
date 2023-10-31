package runner

import (
	"bytes"
	"context"
	"log"

	"github.com/sre-norns/urth/pkg/urth"
)

func runHarScript(ctx context.Context, scriptContent []byte, options RunOptions) (urth.FinalRunResults, error) {
	log.Println("replaying HAR file")

	harLog, err := UnmarshalHAR(bytes.NewReader(scriptContent))
	if err != nil {
		log.Println("...failed to deserialize HAR file: ", err)
		return urth.NewRunResults(urth.RunFinishedError), err
	}

	requests, err := ConvertHarToHttpTester(harLog.Log.Entries)
	if err != nil {
		log.Println("...failed to convert HAR file requests: ", err)
		return urth.NewRunResults(urth.RunFinishedError), err
	}

	return runHttpRequests(ctx, requests, options)
}
