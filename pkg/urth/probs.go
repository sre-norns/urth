package urth

import (
	"sort"

	"github.com/sre-norns/urth/pkg/prob"
)

// ListProbKinds returns the prob kinds this build knows about, sorted so the
// order is stable for a client rendering a picker.
//
// The list comes from the prob registry, which is populated by importing prober
// packages. A binary that does not link them reports an empty list rather than a
// wrong one.
func ListProbKinds() []ProbKindInfo {
	registered := prob.ListProbs()

	kinds := make([]ProbKindInfo, 0, len(registered))
	for kind, info := range registered {
		kinds = append(kinds, ProbKindInfo{
			Kind:        string(kind),
			Version:     info.Version,
			ContentType: info.ContentType,
			Produce:     info.Produce,
		})
	}

	sort.Slice(kinds, func(i, j int) bool { return kinds[i].Kind < kinds[j].Kind })

	return kinds
}
