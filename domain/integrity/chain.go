package integrity

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/abhipray-cpu/go-audit/domain"
	"github.com/abhipray-cpu/go-audit/domain/port"
)

// Chain manages cryptographic hash chain computation and verification
// for tamper-evident audit trails. Each version record's hash includes
// the previous version's hash, creating a chain that detects any
// modification to historical records.
type Chain struct {
	hasher port.HashComputerPort
	reader port.VersionReaderPort
}

// NewChain creates a new hash chain manager.
func NewChain(hasher port.HashComputerPort, reader port.VersionReaderPort) *Chain {
	return &Chain{hasher: hasher, reader: reader}
}

// hashPayload is the canonical data structure hashed for each version.
type hashPayload struct {
	EntityType   string `json:"entity_type"`
	EntityID     string `json:"entity_id"`
	Version      int64  `json:"version"`
	Data         []byte `json:"data"`
	PreviousHash string `json:"previous_hash"`
}

// ComputeAndStore computes the hash for the given record (including
// the previous version's hash) and sets the Hash and PreviousHash
// fields on the record. The caller is responsible for persisting the
// updated record.
func (c *Chain) ComputeAndStore(ctx context.Context, record *domain.VersionRecord) error {
	// Fetch previous version's hash (empty for version 1).
	var prevHash string
	if record.Version > 1 {
		prev, err := c.reader.GetByVersion(ctx, record.EntityType, record.EntityID, record.Version-1)
		if err != nil {
			return fmt.Errorf("%w: fetch previous version %d: %v", domain.ErrStorage, record.Version-1, err)
		}
		prevHash = prev.Hash
	}

	record.PreviousHash = prevHash

	// Build canonical payload and hash it.
	payload := hashPayload{
		EntityType:   record.EntityType,
		EntityID:     record.EntityID,
		Version:      record.Version,
		Data:         record.Data,
		PreviousHash: prevHash,
	}

	raw, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal hash payload: %w", err)
	}

	hash, err := c.hasher.Compute(ctx, raw)
	if err != nil {
		return fmt.Errorf("compute hash: %w", err)
	}

	record.Hash = hash
	return nil
}

// VerifyIntegrity walks the entire version chain for an entity and
// verifies that every hash is consistent. Returns nil if the chain
// is valid, or an error describing the first tampered record.
func (c *Chain) VerifyIntegrity(ctx context.Context, entityType, entityID string) error {
	records, err := c.reader.ListVersions(ctx, entityType, entityID, port.WithOrderAsc())
	if err != nil {
		return fmt.Errorf("%w: list versions: %v", domain.ErrStorage, err)
	}
	if len(records) == 0 {
		return nil // nothing to verify
	}

	prevHash := ""
	for _, rec := range records {
		// Verify the previous hash pointer.
		if rec.PreviousHash != prevHash {
			return fmt.Errorf(
				"%w: version %d previous_hash mismatch: expected %q, got %q",
				domain.ErrValidation, rec.Version, prevHash, rec.PreviousHash)
		}

		// Re-compute hash from canonical payload.
		payload := hashPayload{
			EntityType:   rec.EntityType,
			EntityID:     rec.EntityID,
			Version:      rec.Version,
			Data:         rec.Data,
			PreviousHash: rec.PreviousHash,
		}

		raw, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("marshal hash payload v%d: %w", rec.Version, err)
		}

		expected, err := c.hasher.Compute(ctx, raw)
		if err != nil {
			return fmt.Errorf("compute hash v%d: %w", rec.Version, err)
		}

		if rec.Hash != expected {
			return fmt.Errorf(
				"%w: version %d hash mismatch: expected %q, got %q (tampering detected)",
				domain.ErrValidation, rec.Version, expected, rec.Hash)
		}

		prevHash = rec.Hash
	}

	return nil
}
