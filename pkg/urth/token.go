package urth

import (
	"math/rand"
)

// Temporary token generation facilities

// APIToken is opaque datum used for auth purposes
type APIToken string

// FIXME: Use better token generation!
const alphaNumBytes = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ1234567890"

// const lettersBytes = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"

func NewRandToken(nBytes int) APIToken {
	b := make([]byte, nBytes)
	for i := range b {
		b[i] = alphaNumBytes[rand.Int63()%int64(len(alphaNumBytes))]
	}

	return APIToken(b)
}
