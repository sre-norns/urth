package main

import (
	"encoding/json"
	"testing"

	"github.com/sre-norns/urth/pkg/urth"
	"github.com/sre-norns/wyrd/pkg/manifest"
	"gopkg.in/yaml.v3"
)

func TestRunnerPolicyManifestRoundTrip(t *testing.T) {
	source := []byte(`apiVersion: urth.sre-norns.com/v1
kind: runners
metadata:
  name: edge
spec:
  active: true
  jobRequirements:
    probeKinds: [http]
    maxDuration: 30s
  workerRequirements:
    version: '>=1.9.0 <2.0.0'
  propagatedLabels:
    region: eu
`)
	m, err := decodeManifest(source)
	if err != nil {
		t.Fatal(err)
	}
	r, err := urth.NewRunner(m)
	if err != nil {
		t.Fatal(err)
	}
	view := runnerView{r}
	for _, format := range []string{"json", "yaml"} {
		var wire []byte
		if format == "json" {
			wire, err = json.Marshal(view)
		} else {
			wire, err = yaml.Marshal(view)
		}
		if err != nil {
			t.Fatal(err)
		}
		decoded, err := decodeManifest(wire)
		if err != nil {
			t.Fatal(err)
		}
		got, err := urth.NewRunner(decoded)
		if err != nil {
			t.Fatal(err)
		}
		if policyJSON(got.Spec) != policyJSON(r.Spec) {
			t.Fatalf("%s loses policy: %s", format, wire)
		}
	}
	if len(view.TableHeader(true)) != len(view.TableRow(true)) {
		t.Fatal("Runner wide columns disagree")
	}
}

func TestWorkerInspectionUsesStoredCapabilities(t *testing.T) {
	w := urth.WorkerInstance{ObjectMeta: manifest.ObjectMeta{Name: "edge-worker"}}
	w.Spec.Capabilities.Version = "1.9.0"
	w.Status.EffectiveCapabilities.Version = "1.10.0"
	view := workerView{w}
	rows := view.TableRow(true)
	if got := rows[len(rows)-1]; got != policyJSON(w.Status.EffectiveCapabilities) {
		t.Fatalf("stored snapshot absent: %v", got)
	}
	if len(view.TableHeader(true)) != len(rows) {
		t.Fatal("Worker wide columns disagree")
	}
}
