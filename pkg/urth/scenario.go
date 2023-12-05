package urth

import (
	"net/http"
)

// Well-know labels for scenarios
const (
	LabelScenarioId          = "scenario.id"
	LabelScenarioVersionedId = "scenario.id.versioned"
	LabelScenarioKind        = "scenario.kind"

	LabelScenarioArtifactKind = "artifact.kind"
	LabelScenarioRunId        = "run.id"
	LabelScenarioRunMessageId = "run.messageId"
)

var kindToMimeMap = map[ProbKind]string{}

func ScriptKindToMimeType(kind ProbKind) string {
	mtype, known := kindToMimeMap[kind]
	if !known {
		return "text/plain"
	}

	return mtype
}

func contentTypeToKind(contentType string) (ProbKind, bool) {
	for k, v := range kindToMimeMap {
		if v == contentType {
			return k, true
		}
	}

	return "", false
}

func GuessScenarioKind(hint string, contentType string, data []byte) ProbKind {
	if hint != "" {
		h := ProbKind(hint)
		_, exists := kindToMimeMap[h]
		if exists {
			return h
		}
	}

	if contentType != "" {
		guess, ok := contentTypeToKind(contentType)
		if ok {
			return guess
		}
	}

	contentType = http.DetectContentType(data)
	guess, _ := contentTypeToKind(contentType)

	return guess
}
