package main

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
)

// randBase64URL returns n bytes of base64url-encoded random data.
func randBase64URL(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		// Cryptographic randomness should never fail; fall back to a fixed
		// placeholder so we never block on Instagram requests.
		return "AAAAAAAAAAAAAAAA"
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// randString returns a random alphanumeric string of length n
// "::" + Math.random().toString(36).substring(2).replace(/\d/g, '').slice(0, 6)).
func randString(n int) string {
	const alphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
	b := make([]byte, n)
	for i := range b {
		ri, err := rand.Int(rand.Reader, big.NewInt(int64(len(alphabet))))
		if err != nil {
			b[i] = 'a'
			continue
		}
		b[i] = alphabet[ri.Int64()]
	}
	return string(b)
}

// irandInt returns a non-negative crypto-random int in [0, max).
func irandInt(max int) int {
	if max <= 0 {
		return 0
	}
	n, err := rand.Int(rand.Reader, big.NewInt(int64(max)))
	if err != nil {
		return 0
	}
	return int(n.Int64())
}

// parseJSONNumber unmarshals a json.RawMessage into float64 (the JSON
// number type). Used when scraping numeric fields like __spin_t that may be
// encoded as either a string or a number.
func parseJSONNumber(raw json.RawMessage) (float64, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return 0, fmt.Errorf("empty json")
	}
	var n float64
	if err := json.Unmarshal(raw, &n); err != nil {
		return 0, err
	}
	return n, nil
}