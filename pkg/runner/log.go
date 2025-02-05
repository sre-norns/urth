package runner

import (
	"log"
	"strings"

	"github.com/sre-norns/urth/pkg/prob"
	"github.com/sre-norns/urth/pkg/urth"
)

type RunLog struct {
	content strings.Builder
}

// RunLog implements io.Writer
func (l *RunLog) Write(p []byte) (n int, err error) {
	n, err = l.content.Write(p)
	log.Writer().Write(p)

	return
}

// func (l *RunLog) Log(v ...any) {
// 	fmt.Fprint(&l.content, v...)
// 	fmt.Fprint(&l.content, "\n")

// 	log.Print(v...)
// }

// func (l *RunLog) Logf(format string, v ...any) {
// 	l.Log(fmt.Sprintf(format, v...))
// }

func (l *RunLog) ToArtifact() urth.ArtifactSpec {
	return urth.ArtifactSpec{
		Artifact: prob.Artifact{
			Rel:      "log",
			MimeType: "text/plain",
			Content:  []byte(l.content.String()),
		},
	}
}

// func (l *RunLog) Package() []urth.ArtifactSpec {
// 	return []urth.ArtifactSpec{l.ToArtifact()}
// }
