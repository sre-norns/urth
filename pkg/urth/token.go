package urth

import (
	"math/rand"
)

// Temporary token generation facilities

// ApiToken is opaque datum used for auth purposes
type ApiToken string

// FIXME: Use better token generation!
const alphaNumBytes = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ1234567890"
const lettersBytes = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"

func NewRandToken(nBytes int) ApiToken {
	b := make([]byte, nBytes)
	for i := range b {
		b[i] = alphaNumBytes[rand.Int63()%int64(len(alphaNumBytes))]
	}

	return ApiToken(b)
}
