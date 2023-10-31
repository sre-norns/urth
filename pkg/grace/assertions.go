package grace

import "fmt"

type Error interface {
	WhatExpected() string
	WhatHappened() string
	WhatToDo() string
}

type ActionableError struct {
	expected     string
	got          string
	callToAction string
}

func (e *ActionableError) WhatExpected() string {
	return e.expected
}

func (e *ActionableError) WhatHappened() string {
	return e.got
}

func (e *ActionableError) WhatToDo() string {
	return e.callToAction
}

func (e *ActionableError) Error() string {
	return fmt.Sprintf("expected: %s, got: %s; What to do: %s", e.expected, e.got, e.callToAction)
}

func RaiseError(
	expected, got, cta string,
) Error {
	return &ActionableError{
		expected:     expected,
		got:          got,
		callToAction: cta,
	}
}
