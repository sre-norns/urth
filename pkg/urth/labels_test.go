package urth

import (
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sre-norns/urth/pkg/prob"
	"github.com/sre-norns/wyrd/pkg/manifest"
)

// The data-class labels are applied to every artifact on upload, so an invalid
// key or value would not be a cosmetic problem: it would reject the artifact.
func TestArtifactDataClassLabelsAreValid(t *testing.T) {
	for _, class := range []prob.DataClass{
		prob.DataClassUnknown,
		prob.DataClassClean,
		prob.DataClassRedacted,
		prob.DataClassSecretBearing,
	} {
		t.Run(class.String(), func(t *testing.T) {
			labels := manifest.Labels{
				LabelArtifactDataClass:         class.String(),
				LabelArtifactMayContainSecrets: strconv.FormatBool(class.MayContainSecrets()),
			}

			require.NoError(t, labels.Validate())
		})
	}
}

// An artifact that declares nothing must not be reported as safe: a prober that
// forgets to classify its output should fail towards caution, not away from it.
func TestUnclassifiedArtifactIsTreatedAsUnsafe(t *testing.T) {
	var undeclared prob.DataClass

	require.Equal(t, prob.DataClassUnknown, undeclared)
	require.True(t, undeclared.MayContainSecrets())
	require.Equal(t, "unknown", undeclared.String(),
		"unknown must be spelled out so it is selectable in a label query")
}

func TestDataClassMayContainSecrets(t *testing.T) {
	testCases := map[prob.DataClass]bool{
		prob.DataClassClean:         false,
		prob.DataClassRedacted:      false,
		prob.DataClassSecretBearing: true,
		prob.DataClassUnknown:       true,
		prob.DataClass("nonsense"):  true,
	}

	for class, expect := range testCases {
		t.Run(class.String(), func(t *testing.T) {
			require.Equal(t, expect, class.MayContainSecrets())
		})
	}
}

// A worker uploads artifacts and supplies its own labels with them. The
// data-class labels are what retention and audits act on, so a worker must not
// be able to relabel a secret-bearing HAR as clean.
func TestArtifactLabelsCannotBeOverriddenByWorker(t *testing.T) {
	spec := ArtifactSpec{
		Artifact: prob.Artifact{
			Rel:       "har",
			MimeType:  "application/json",
			DataClass: prob.DataClassSecretBearing,
		},
	}

	workerSupplied := manifest.Labels{
		LabelArtifactDataClass:         string(prob.DataClassClean),
		LabelArtifactMayContainSecrets: "false",
		LabelArtifactKind:              "not-a-har",
		"team":                         "checkout",
	}

	labels := artifactLabels(workerSupplied, spec, Result{})

	require.Equal(t, "secret-bearing", labels[LabelArtifactDataClass])
	require.Equal(t, "true", labels[LabelArtifactMayContainSecrets])
	require.Equal(t, "har", labels[LabelArtifactKind])

	// Labels the system does not own are the worker's to set.
	require.Equal(t, "checkout", labels["team"])
}

// The classification is read from the parsed spec, so an artifact that declares
// nothing is labelled unknown rather than silently omitted from an audit.
func TestArtifactLabelsClassifyUndeclaredAsUnknown(t *testing.T) {
	labels := artifactLabels(nil, ArtifactSpec{
		Artifact: prob.Artifact{Rel: "log", MimeType: "text/plain"},
	}, Result{})

	require.Equal(t, "unknown", labels[LabelArtifactDataClass])
	require.Equal(t, "true", labels[LabelArtifactMayContainSecrets])
	require.NoError(t, labels.Validate())
}

// Several artifact labels are derived from values that were never constrained to
// the label grammar. Labels are validated when a manifest is parsed, which is
// before the server merges these in, so a malformed value is persisted rather
// than rejected -- and then quietly fails to match any audit query.
func TestArtifactLabelsAreValidForRealisticInputs(t *testing.T) {
	testCases := map[string]ArtifactSpec{
		"har": {Artifact: prob.Artifact{
			Rel: "har", MimeType: "application/json", DataClass: prob.DataClassSecretBearing,
		}},
		"log": {Artifact: prob.Artifact{
			Rel: "log", MimeType: "text/plain", DataClass: prob.DataClassRedacted,
		}},
		"metrics": {Artifact: prob.Artifact{
			Rel: "metrics", MimeType: "text/plain; version=0.0.4; charset=utf-8", DataClass: prob.DataClassClean,
		}},
		// puppeteer derives an artifact's kind from a file extension
		"puppeteer screenshot": {Artifact: prob.Artifact{
			Rel: ".png", MimeType: "image/png", DataClass: prob.DataClassUnknown,
		}},
		"structured mime": {Artifact: prob.Artifact{
			Rel: "report", MimeType: "application/vnd.api+json", DataClass: prob.DataClassUnknown,
		}},
		"no mime type": {Artifact: prob.Artifact{
			Rel: "log", DataClass: prob.DataClassRedacted,
		}},
	}

	for name, spec := range testCases {
		t.Run(name, func(t *testing.T) {
			labels := artifactLabels(nil, spec, Result{})

			require.NoError(t, labels.Validate())
			require.Equal(t, spec.Artifact.DataClass.String(), labels[LabelArtifactDataClass])
		})
	}
}

func TestLabelSafeValue(t *testing.T) {
	testCases := map[string]string{
		"text/plain":                "text-plain",
		"application/vnd.api+json":  "application-vnd.api-json",
		".png":                      "png",
		"har":                       "har",
		"":                          "",
		"...":                       "",
		"text/plain; charset=utf-8": "text-plain--charset-utf-8",
		// A binary built from a dirty working tree -- i.e. every developer's --
		// reports this, and the '+' made worker registration fail outright.
		"v0.0.0-20260720134510-2c9cd4c33f2d+dirty": "v0.0.0-20260720134510-2c9cd4c33f2d-dirty",
		"(devel)": "devel",
		"v1.2.3":  "v1.2.3",
	}

	for input, expect := range testCases {
		t.Run(input, func(t *testing.T) {
			require.Equal(t, expect, LabelSafeValue(input))
		})
	}
}

// A value that cannot be represented is omitted rather than written empty: an
// empty label value is not valid either.
func TestArtifactLabelsOmitUnrepresentableValues(t *testing.T) {
	labels := artifactLabels(nil, ArtifactSpec{
		Artifact: prob.Artifact{Rel: "log", MimeType: "///"},
	}, Result{})

	require.NotContains(t, labels, LabelArtifactMime)
	require.NoError(t, labels.Validate())
}
