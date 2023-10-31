package urth

import "net/http"

type ScenarioKind string

const (
	TcpPortCheckKind ScenarioKind = "tcp/port"
	HttpGetKind      ScenarioKind = "http/get"
	HarKind          ScenarioKind = "http/har"
	PuppeteerKind    ScenarioKind = "puppeteer/javascript"
	PyPuppeteerKind  ScenarioKind = "puppeteer/python"
)

var kindToMimeMap = map[ScenarioKind]string{
	TcpPortCheckKind: "text/plain",
	HttpGetKind:      "application/http",
	HarKind:          "application/json",
	PuppeteerKind:    "text/javascript",
	PyPuppeteerKind:  "text/x-python",
}

func ScriptKindToMimeType(kind ScenarioKind) string {
	mtype, known := kindToMimeMap[kind]
	if !known {
		return "text/plain"
	}

	return mtype
}

func contentTypeToKind(contentType string) (ScenarioKind, bool) {
	for k, v := range kindToMimeMap {
		if v == contentType {
			return k, true
		}
	}

	return PyPuppeteerKind, false
}

func GuessScenarioKind(hint string, contentType string, data []byte) ScenarioKind {
	if hint != "" {
		for k := range kindToMimeMap {
			if k == ScenarioKind(hint) {
				return k
			}
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
