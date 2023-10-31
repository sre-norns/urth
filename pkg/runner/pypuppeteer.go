package runner

import (
	"context"
	"fmt"
	"log"

	"github.com/sre-norns/urth/pkg/urth"
)

func runPyPuppeteerScript(ctx context.Context, scriptContent []byte, options RunOptions) (urth.FinalRunResults, error) {
	log.Println("FIXME: PyPuppeteer scenarios are not implemented....yet")

	return urth.NewRunResults(urth.RunFinishedError), fmt.Errorf("not implemented yet")
}
