package api

import (
	"crypto/rand"
	"encoding/hex"
)

// NewIdempotencyKey returns a random version 4 UUID for the Idempotency-Key
// header. The API deduplicates POST, PATCH and PUT requests on that header, so
// every write invocation must generate a fresh key.
//
// crypto/rand.Read never reports an error, it panics if the system source is
// broken, so there is no failure path to surface here.
func NewIdempotencyKey() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	h := hex.EncodeToString(b[:])
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32]
}
