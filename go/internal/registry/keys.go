// Package registry resolves API keys to rate-limit identities
// (ARCHITECTURE.md section 10). Keys are registered in Redis by hash; the
// raw key is only ever seen by the client and the CLI that creates it.
package registry

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/big"
)

const (
	keyPrefix  = "rlk_"
	keyBodyLen = 32
	base62     = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
)

// GenerateKey returns a new random key: "rlk_" + 32 base62 characters
// (~190 bits of entropy).
func GenerateKey() (string, error) {
	body := make([]byte, keyBodyLen)
	alphabet := big.NewInt(int64(len(base62)))
	for i := range body {
		n, err := rand.Int(rand.Reader, alphabet)
		if err != nil {
			return "", fmt.Errorf("generating key: %w", err)
		}
		body[i] = base62[n.Int64()]
	}
	return keyPrefix + string(body), nil
}

// ValidFormat reports whether key could have been issued by GenerateKey.
// Anything else is treated as unregistered without a Redis lookup.
func ValidFormat(key string) bool {
	if len(key) != len(keyPrefix)+keyBodyLen || key[:len(keyPrefix)] != keyPrefix {
		return false
	}
	for i := len(keyPrefix); i < len(key); i++ {
		if !isBase62(key[i]) {
			return false
		}
	}
	return true
}

func isBase62(c byte) bool {
	return '0' <= c && c <= '9' || 'A' <= c && c <= 'Z' || 'a' <= c && c <= 'z'
}

// Hash is the registry's storage form of a key.
func Hash(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}
