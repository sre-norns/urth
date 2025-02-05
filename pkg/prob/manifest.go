package prob

import (
	"fmt"
	"reflect"

	"github.com/sre-norns/wyrd/pkg/manifest"
)

var probKindRegistry = map[Kind]reflect.Type{}

func RegisterKind(kind Kind, proto any) error {
	val := reflect.ValueOf(proto)
	if !val.CanInterface() {
		return fmt.Errorf("type of %q can not interface", val.Type())
	}

	t := val.Type()
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}

	probKindRegistry[kind] = t
	return nil
}

func UnregisterKind(kind Kind) {
	delete(probKindRegistry, kind)
}

func InstanceOf(kind manifest.Kind) (manifest.ResourceManifest, error) {
	t, known := probKindRegistry[kind]
	if !known {
		return manifest.ResourceManifest{}, fmt.Errorf("%w: %q", manifest.ErrUnknownKind, kind)
	}

	return manifest.ResourceManifest{
		TypeMeta: manifest.TypeMeta{
			Kind: kind,
		},
		Spec: reflect.New(t).Interface(),
	}, nil
}
