package main

import (
	"encoding/json"
	"fmt"

	"github.com/sre-norns/wyrd/pkg/manifest"
	"gopkg.in/yaml.v3"
)

func manifestFromFile(filename string) (result manifest.ResourceManifest, ok bool, err error) {
	content, ext, err := readContent(filename)
	if err != nil {
		return result, false, fmt.Errorf("failed to read manifest content from `%v`: %w", filename, err)
	}

	switch ext {
	case ".json":
		err = json.Unmarshal(content, &result)
		return result, true, err
	case ".yaml", ".yml":
		err = yaml.Unmarshal(content, &result)
		return result, true, err
	}

	return result, false, err
}
