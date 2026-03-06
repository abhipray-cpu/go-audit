package hash

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
)

// SHA256 implements [port.HashComputerPort] using SHA-256.
type SHA256 struct{}

// NewSHA256 returns a new SHA-256 hash computer.
func NewSHA256() *SHA256 { return &SHA256{} }

// Compute returns the SHA-256 hash of data as a lowercase hex string.
func (s *SHA256) Compute(_ context.Context, data []byte) (string, error) {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:]), nil
}
