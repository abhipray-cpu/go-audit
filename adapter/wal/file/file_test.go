package file

import (
	"context"
	"encoding/binary"
	"hash/crc32"
	"os"
	"testing"
	"time"

	"github.com/abhipray-cpu/go-audit/domain"
	"github.com/abhipray-cpu/go-audit/domain/port"
)

func makeEntry(id string, version int64) port.WALEntry {
	return port.WALEntry{
		ID: id,
		Record: domain.VersionRecord{
			ID:         "rec-" + id,
			EntityType: "user",
			EntityID:   "u1",
			Version:    version,
		},
		CreatedAt: time.Now(),
	}
}

func TestFileWAL_AppendAndReplay(t *testing.T) {
	dir := t.TempDir()
	w, err := New(dir)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer w.Close()

	ctx := context.Background()

	// Append 3 entries.
	for i := 1; i <= 3; i++ {
		if err := w.Append(ctx, makeEntry("e"+string(rune('0'+i)), int64(i))); err != nil {
			t.Fatalf("Append(%d) error: %v", i, err)
		}
	}

	// Replay should return all 3.
	entries, err := w.Replay(ctx)
	if err != nil {
		t.Fatalf("Replay() error: %v", err)
	}
	if len(entries) != 3 {
		t.Errorf("Replay() returned %d entries, want 3", len(entries))
	}
}

func TestFileWAL_AckRemovesFromReplay(t *testing.T) {
	dir := t.TempDir()
	w, err := New(dir)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer w.Close()

	ctx := context.Background()

	// Append 3 entries.
	w.Append(ctx, makeEntry("e1", 1))
	w.Append(ctx, makeEntry("e2", 2))
	w.Append(ctx, makeEntry("e3", 3))

	// Ack e1 and e3.
	w.Ack(ctx, "e1")
	w.Ack(ctx, "e3")

	// Replay should return only e2.
	entries, err := w.Replay(ctx)
	if err != nil {
		t.Fatalf("Replay() error: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("Replay() returned %d entries, want 1", len(entries))
	}
	if entries[0].ID != "e2" {
		t.Errorf("Replay()[0].ID = %q, want %q", entries[0].ID, "e2")
	}
}

func TestFileWAL_CRC32Integrity(t *testing.T) {
	dir := t.TempDir()
	w, err := New(dir)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	ctx := context.Background()

	// Append a valid entry.
	w.Append(ctx, makeEntry("e1", 1))
	w.Close()

	// Corrupt the CRC in the WAL file.
	// Entry format: [4 len] [N payload] [4 crc]
	data, err := os.ReadFile(dir + "/wal.dat")
	if err != nil {
		t.Fatalf("ReadFile error: %v", err)
	}

	if len(data) < 8 {
		t.Fatalf("WAL file too small: %d bytes", len(data))
	}

	// Flip a bit in the CRC (last 4 bytes).
	data[len(data)-1] ^= 0xFF

	os.WriteFile(dir+"/wal.dat", data, 0o644)

	// Re-open and replay — corrupt entry should be skipped.
	w2, err := New(dir)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer w2.Close()

	entries, err := w2.Replay(ctx)
	if err != nil {
		t.Fatalf("Replay() error: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("Replay() returned %d entries after CRC corruption, want 0", len(entries))
	}
}

func TestFileWAL_CrashRecovery(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	// Phase 1: Write entries, ack some, then "crash" (close without cleanup).
	func() {
		w, err := New(dir)
		if err != nil {
			t.Fatalf("New() error: %v", err)
		}

		w.Append(ctx, makeEntry("e1", 1))
		w.Append(ctx, makeEntry("e2", 2))
		w.Append(ctx, makeEntry("e3", 3))
		w.Ack(ctx, "e1")

		// Simulate crash — just close.
		w.Close()
	}()

	// Phase 2: Re-open — should see e2 and e3 (e1 was acked).
	w2, err := New(dir)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer w2.Close()

	entries, err := w2.Replay(ctx)
	if err != nil {
		t.Fatalf("Replay() error: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("Replay() returned %d entries, want 2 (e2, e3)", len(entries))
	}

	// Verify IDs.
	ids := map[string]bool{}
	for _, e := range entries {
		ids[e.ID] = true
	}
	if !ids["e2"] {
		t.Error("expected e2 in replay")
	}
	if !ids["e3"] {
		t.Error("expected e3 in replay")
	}
}

func TestFileWAL_TruncatedEntry(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	w, err := New(dir)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	// Write a valid entry.
	w.Append(ctx, makeEntry("e1", 1))
	w.Close()

	// Append a truncated entry: write just the length prefix (simulate crash
	// mid-write).
	f, _ := os.OpenFile(dir+"/wal.dat", os.O_WRONLY|os.O_APPEND, 0o644)
	var lenBuf [4]byte
	binary.BigEndian.PutUint32(lenBuf[:], 100)
	f.Write(lenBuf[:])
	f.Close()

	// Re-open — should still see e1, truncated entry skipped.
	w2, err := New(dir)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer w2.Close()

	entries, err := w2.Replay(ctx)
	if err != nil {
		t.Fatalf("Replay() error: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("Replay() returned %d entries, want 1", len(entries))
	}
	if entries[0].ID != "e1" {
		t.Errorf("entry ID = %q, want %q", entries[0].ID, "e1")
	}
}

func TestFileWAL_EmptyReplay(t *testing.T) {
	dir := t.TempDir()
	w, err := New(dir)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer w.Close()

	entries, err := w.Replay(context.Background())
	if err != nil {
		t.Fatalf("Replay() error: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("Replay() returned %d entries on empty WAL, want 0", len(entries))
	}
}

func TestFileWAL_CRC32Verification(t *testing.T) {
	// Verify that the CRC stored in the file matches a manually computed checksum.
	dir := t.TempDir()
	w, err := New(dir)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	w.Append(context.Background(), makeEntry("verify", 42))
	w.Close()

	data, _ := os.ReadFile(dir + "/wal.dat")

	// Parse: [4 len] [N payload] [4 crc]
	payloadLen := binary.BigEndian.Uint32(data[:4])
	payload := data[4 : 4+payloadLen]
	storedCRC := binary.BigEndian.Uint32(data[4+payloadLen : 4+payloadLen+4])

	computedCRC := crc32.ChecksumIEEE(payload)
	if storedCRC != computedCRC {
		t.Errorf("CRC mismatch: stored=%d, computed=%d", storedCRC, computedCRC)
	}
}

func BenchmarkFileWAL_Append(b *testing.B) {
	dir := b.TempDir()
	w, err := New(dir)
	if err != nil {
		b.Fatalf("New() error: %v", err)
	}
	defer w.Close()

	entry := makeEntry("bench", 1)
	ctx := context.Background()

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		entry.ID = string(rune(i))
		w.Append(ctx, entry)
	}
}
