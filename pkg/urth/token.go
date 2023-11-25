package urth

import "math/rand"

// Temporary token generation facilities

// FIXME: Use better token generation!
const letterBytes = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"

func RandToken(n int) ApiToken {
	b := make([]byte, n)
	for i := range b {
		b[i] = letterBytes[rand.Int63()%int64(len(letterBytes))]
	}
	return ApiToken(b)
}
