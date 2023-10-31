package grace

import (
	"context"
	"errors"
	"log"
)

func ExitOrLog(err error) {
	if err != nil && !errors.Is(err, context.Canceled) {
		log.Fatal(err)
	}
}
