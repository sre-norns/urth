package urth

import (
	"net/http"

	"github.com/sre-norns/urth/pkg/prob"
)

var kindToMimeMap = map[prob.Kind]string{}

func ScriptKindToMimeType(kind prob.Kind) string {
	mtype, known := kindToMimeMap[kind]
	if !known {
		return "text/plain"
	}

	return mtype
}

func contentTypeToKind(contentType string) (prob.Kind, bool) {
	for k, v := range kindToMimeMap {
		if v == contentType {
			return k, true
		}
	}

	return "", false
}

func GuessScenarioKind(hint string, contentType string, data []byte) prob.Kind {
	if hint != "" {
		h := prob.Kind(hint)
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
