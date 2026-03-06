package port

import (
	"context"
)

// HashComputerPort computes cryptographic hashes for version records.
// Used by the integrity package to build tamper-evident hash chains.
type HashComputerPort interface {
	// Compute returns the hash of the given data as a hex-encoded string.
	Compute(ctx context.Context, data []byte) (string, error)
}
