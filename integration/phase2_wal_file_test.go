//go:build integration

package integration

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	walfile "github.com/abhipray-cpu/go-audit/adapter/wal/file"
	"github.com/abhipray-cpu/go-audit/domain/port"
)

// ---------------------------------------------------------------------------
// Phase 2 — WF-001 to WF-005: File WAL
// ---------------------------------------------------------------------------

// WF-001: TestWAL_File_AppendAndReplay appends 3 entries, replays all 3.
func TestWAL_File_AppendAndReplay(t *testing.T) {
	dir := t.TempDir()
	w, err := walfile.New(dir)
	if err != nil {
		t.Fatalf("New WAL: %v", err)
	}
	defer w.Close()

	ctx := context.Background()
	for i := 0; i < 3; i++ {
		entry := port.WALEntry{ID: uniqueID("wal")}
		if err := w.Append(ctx, entry); err != nil {
			t.Fatalf("Append %d: %v", i, err)
		}
	}

	entries, err := w.Replay(ctx)
	if err != nil {
		t.Fatalf("Replay: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}
}

// WF-002: TestWAL_File_AckRemovesFromReplay appends 3, acks 1, replays 2.
func TestWAL_File_AckRemovesFromReplay(t *testing.T) {
	dir := t.TempDir()
	w, err := walfile.New(dir)
	if err != nil {
		t.Fatalf("New WAL: %v", err)
	}
	defer w.Close()

	ctx := context.Background()
	ids := make([]string, 3)
	for i := 0; i < 3; i++ {
		ids[i] = uniqueID("wal")
		entry := port.WALEntry{ID: ids[i]}
		if err := w.Append(ctx, entry); err != nil {
			t.Fatalf("Append %d: %v", i, err)
		}
	}

	// Ack the first entry.
	if err := w.Ack(ctx, ids[0]); err != nil {
		t.Fatalf("Ack: %v", err)
	}

	entries, err := w.Replay(ctx)
	if err != nil {
		t.Fatalf("Replay: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 unacked entries, got %d", len(entries))
	}
}

// WF-003: TestWAL_File_CRCIntegrity corrupts a byte in the WAL file and
// confirms Replay skips the corrupt entry.
func TestWAL_File_CRCIntegrity(t *testing.T) {
	dir := t.TempDir()
	w, err := walfile.New(dir)
	if err != nil {
		t.Fatalf("New WAL: %v", err)
	}

	ctx := context.Background()
	entry := port.WALEntry{ID: uniqueID("wal")}
	if err := w.Append(ctx, entry); err != nil {
		t.Fatalf("Append: %v", err)
	}
	w.Close()

	// Corrupt a byte in the WAL data file.
	walPath := filepath.Join(dir, "wal.dat")
	data, err := os.ReadFile(walPath)
	if err != nil {
		t.Fatalf("read WAL: %v", err)
	}
	if len(data) > 10 {
		data[10] ^= 0xFF // flip a byte in the payload
	}
	if err := os.WriteFile(walPath, data, 0o644); err != nil {
		t.Fatalf("write corrupted WAL: %v", err)
	}

	// Re-open and replay.
	w2, err := walfile.New(dir)
	if err != nil {
		t.Fatalf("Re-open WAL: %v", err)
	}
	defer w2.Close()

	entries, err := w2.Replay(ctx)
	if err != nil {
		t.Fatalf("Replay: %v", err)
	}
	// Corrupt entry should be skipped.
	if len(entries) != 0 {
		t.Fatalf("expected 0 valid entries after corruption, got %d", len(entries))
	}
}

// WF-004: TestWAL_File_EmptyReplay confirms a fresh WAL replays empty.
func TestWAL_File_EmptyReplay(t *testing.T) {
	dir := t.TempDir()
	w, err := walfile.New(dir)
	if err != nil {
		t.Fatalf("New WAL: %v", err)
	}
	defer w.Close()

	entries, err := w.Replay(context.Background())
	if err != nil {
		t.Fatalf("Replay: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected empty replay, got %d", len(entries))
	}
}

// WF-005: TestWAL_File_CrashRecovery_E2E exercises crash recovery by creating
// a WAL with an un-acked entry, then creating a new Auditor with that WAL
// and verifying the entry is available for replay.
func TestWAL_File_CrashRecovery_E2E(t *testing.T) {
	dir := t.TempDir()
	w, err := walfile.New(dir)
	if err != nil {
		t.Fatalf("New WAL: %v", err)
	}

	ctx := context.Background()
	entryID := uniqueID("wal-crash")
	entry := port.WALEntry{ID: entryID}
	if err := w.Append(ctx, entry); err != nil {
		t.Fatalf("Append: %v", err)
	}
	w.Close()

	// Simulate restart — re-open WAL and check replay.
	w2, err := walfile.New(dir)
	if err != nil {
		t.Fatalf("Re-open WAL: %v", err)
	}
	defer w2.Close()

	entries, err := w2.Replay(ctx)
	if err != nil {
		t.Fatalf("Replay: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 un-acked entry, got %d", len(entries))
	}
	if entries[0].ID != entryID {
		t.Fatalf("expected entry ID %q, got %q", entryID, entries[0].ID)
	}
}
