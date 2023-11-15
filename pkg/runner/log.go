package runner

import (
	"fmt"
	"log"
	"strings"

	"github.com/sre-norns/urth/pkg/urth"
)

type RunLog struct {
	content strings.Builder
}

func (l *RunLog) Log(v ...any) {
	logLine := fmt.Sprint(v...)
	_, _ = l.content.WriteString(logLine)
	_, _ = l.content.WriteString("\n")

	log.Print(logLine)
}

func (l *RunLog) Logf(format string, v ...any) {
	l.Log(fmt.Sprintf(format, v...))
}

func (l *RunLog) ToArtifact() urth.ArtifactValue {
	return urth.ArtifactValue{
		Rel:      "log",
		MimeType: "text/plain",
		Content:  []byte(l.content.String()),
	}
}

func NewRunResultsWithLog(runResult urth.RunStatus, logger *RunLog, options ...urth.RunResultOption) urth.FinalRunResults {
	return urth.NewRunResults(
		urth.RunFinishedSuccess,
		append([]urth.RunResultOption{urth.WithArtifacts(logger.ToArtifact())}, options...)...,
	)
}
