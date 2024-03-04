# Wyrd

A collection of components to help define flexible resource manifests. Inspired by Kubernetes Custom resource definition format.

# Usage

Suppose you define your custom type in the code:
```go
//....
type MySpec struct {
    Value int    `yaml:"value"`
    Name  string `yaml:"name"`
}

//... Somewhere, usually as init()

	err := wyrd.RegisterKind(wyrd.Kind("mySpec"), &MySpec{})
```

A manifest for a resource that can be unmarshaled into the type above would looks like:
```yaml
kind: mySpec
metadata:
    name: test-spec
spec:
    value: 42
    name: meaning
```

Note that a resource manfiest "wraps" type definition. A `ResourceManifest` consists of type meta information to help marshaling,
information about the object instance, such as `UUID`, `Name` etc. and the custom type spec. itself.

```go
type ResourceManifest struct {
	TypeMeta `json:",inline" yaml:",inline"`
	Metadata ObjectMeta `json:"metadata" yaml:"metadata"`
	Spec     any        `json:"-" yaml:"-"`
}
```
