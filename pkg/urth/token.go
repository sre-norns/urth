package urth

import (
	"math/rand"
	"time"
)

// Temporary token generation facilities

// FIXME: Use better token generation!
const letterBytes = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"

func randToken(n int) ApiToken {
	rand.Seed(time.Now().UnixNano())

	b := make([]byte, n)
	for i := range b {
		b[i] = letterBytes[rand.Int63()%int64(len(letterBytes))]
	}
	return ApiToken(b)
}
