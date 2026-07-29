package main

import (
	"testing"

	"github.com/sre-norns/urth/pkg/probers/puppeteer"
)

// The script endpoint answered 404 for every scenario, including ones that
// plainly had a script, and had done since the api-server started linking
// probers in: it asserted the prob spec to map[string]any, which stopped being
// true the moment specs decoded to their registered types. Nothing caught it
// because nothing tested the extraction against a *typed* spec -- which is the
// only kind the api-server ever sees.

func TestProbScriptReadsATypedSpec(t *testing.T) {
	const script = "await page.goto('https://example.com')"

	cases := []struct {
		name string
		spec any
		want string
		ok   bool
	}{
		{
			name: "pointer to a registered spec",
			spec: &puppeteer.Spec{Script: script},
			want: script,
			ok:   true,
		},
		{
			name: "spec value, not a pointer",
			spec: puppeteer.Spec{Script: script},
			want: script,
			ok:   true,
		},
		{
			name: "a registered spec with no script set",
			spec: &puppeteer.Spec{},
		},
		{
			name: "an unregistered kind still arrives as a map",
			spec: map[string]any{"script": script},
			want: script,
			ok:   true,
		},
		{
			name: "a map authored with Go field names",
			spec: map[string]any{"Script": script},
			want: script,
			ok:   true,
		},
		{
			name: "a kind that carries no script at all",
			spec: struct{ Target string }{Target: "localhost:80"},
		},
		{
			name: "a nil spec is not a panic",
			spec: nil,
		},
		{
			name: "a spec whose Script is not a string",
			spec: struct{ Script int }{Script: 7},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := probScript(tc.spec)

			if ok != tc.ok {
				t.Fatalf("probScript(%T) ok = %v, want %v", tc.spec, ok, tc.ok)
			}
			if got != tc.want {
				t.Errorf("probScript(%T) = %q, want %q", tc.spec, got, tc.want)
			}
		})
	}
}
