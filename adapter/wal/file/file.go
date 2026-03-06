// Package file provides a file-based [port.WALPort] implementation for go-audit.
//
// FileWAL writes version records to a local append-only file with fsync
// durability and CRC32 integrity checks on every entry. Acknowledged entries
// are tracked in a companion ".ack" file so that [WAL.Replay] only returns
// un-acknowledged entries after a crash.
//
// # File Format
//
// Each WAL entry is written as:
//
//	[4 bytes: payload length (big-endian uint32)]
//	[N bytes: JSON-encoded WALEntry]
//	[4 bytes: CRC32 checksum of the JSON payload (big-endian uint32)]
//
// Acknowledged entry IDs are appended to a ".ack" sidecar file, one ID per
// line. On [WAL.Replay], entries whose IDs appear in the ack file are skipped.
//
// # Thread Safety
//
// All methods are safe for concurrent use; writes are serialized by a mutex.
package file

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"strings"
	"sync"

	"github.com/abhipray-cpu/go-audit/domain/port"
)

// Compile-time check.
var _ port.WALPort = (*WAL)(nil)

// WAL is a file-based Write-Ahead Log with CRC32 integrity and fsync
// durability. Create one with [New].
type WAL struct {
	mu      sync.Mutex
	walPath string
	ackPath string
	walFile *os.File
	ackFile *os.File
}

// New opens (or creates) the WAL files at the given directory path.
// The WAL data file is named "wal.dat" and the ack sidecar is "wal.ack".
func New(dir string) (*WAL, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("filewal: mkdir %s: %w", dir, err)
	}

	walPath := dir + "/wal.dat"
	ackPath := dir + "/wal.ack"

	walFile, err := os.OpenFile(walPath, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0o644)
	if err != nil {
		return nil, fmt.Errorf("filewal: open wal: %w", err)
	}

	ackFile, err := os.OpenFile(ackPath, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0o644)
	if err != nil {
		walFile.Close()
		return nil, fmt.Errorf("filewal: open ack: %w", err)
	}

	return &WAL{
		walPath: walPath,
		ackPath: ackPath,
		walFile: walFile,
		ackFile: ackFile,
	}, nil
}

// Append writes a WAL entry to the append-only file and calls fsync.
//
// Wire format per entry:
//
//	[4 bytes: payload length] [N bytes: JSON payload] [4 bytes: CRC32]
func (w *WAL) Append(_ context.Context, entry port.WALEntry) error {
	payload, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("filewal: marshal entry: %w", err)
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	// Write length prefix.
	var lenBuf [4]byte
	binary.BigEndian.PutUint32(lenBuf[:], uint32(len(payload)))
	if _, err := w.walFile.Write(lenBuf[:]); err != nil {
		return fmt.Errorf("filewal: write length: %w", err)
	}

	// Write payload.
	if _, err := w.walFile.Write(payload); err != nil {
		return fmt.Errorf("filewal: write payload: %w", err)
	}

	// Write CRC32 checksum.
	checksum := crc32.ChecksumIEEE(payload)
	var crcBuf [4]byte
	binary.BigEndian.PutUint32(crcBuf[:], checksum)
	if _, err := w.walFile.Write(crcBuf[:]); err != nil {
		return fmt.Errorf("filewal: write crc: %w", err)
	}

	// fsync for durability.
	if err := w.walFile.Sync(); err != nil {
		return fmt.Errorf("filewal: fsync: %w", err)
	}

	return nil
}

// Ack marks a WAL entry as successfully persisted by appending its ID
// to the ack sidecar file.
func (w *WAL) Ack(_ context.Context, entryID string) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	line := entryID + "\n"
	if _, err := w.ackFile.WriteString(line); err != nil {
		return fmt.Errorf("filewal: write ack: %w", err)
	}
	if err := w.ackFile.Sync(); err != nil {
		return fmt.Errorf("filewal: fsync ack: %w", err)
	}

	return nil
}

// Replay reads all WAL entries from the data file and returns only those
// that have NOT been acknowledged. Entries with CRC32 mismatches are
// silently skipped (truncated writes from a crash).
func (w *WAL) Replay(_ context.Context) ([]port.WALEntry, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	// 1. Load acknowledged IDs.
	acked, err := w.loadAcked()
	if err != nil {
		return nil, fmt.Errorf("filewal: load acked: %w", err)
	}

	// 2. Read all entries from WAL file.
	entries, err := w.readEntries()
	if err != nil {
		return nil, fmt.Errorf("filewal: read entries: %w", err)
	}

	// 3. Filter out acknowledged entries.
	var unacked []port.WALEntry
	for _, e := range entries {
		if !acked[e.ID] {
			unacked = append(unacked, e)
		}
	}

	return unacked, nil
}

// Close closes the WAL and ack files.
func (w *WAL) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	var errs []string
	if err := w.walFile.Close(); err != nil {
		errs = append(errs, fmt.Sprintf("wal: %v", err))
	}
	if err := w.ackFile.Close(); err != nil {
		errs = append(errs, fmt.Sprintf("ack: %v", err))
	}
	if len(errs) > 0 {
		return fmt.Errorf("filewal: close: %s", strings.Join(errs, "; "))
	}
	return nil
}

// loadAcked reads the ack sidecar file and returns a set of acknowledged IDs.
func (w *WAL) loadAcked() (map[string]bool, error) {
	data, err := os.ReadFile(w.ackPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	acked := make(map[string]bool)
	for _, line := range strings.Split(string(data), "\n") {
		id := strings.TrimSpace(line)
		if id != "" {
			acked[id] = true
		}
	}
	return acked, nil
}

// readEntries reads all valid entries from the WAL data file.
// Entries with CRC32 mismatches (from truncated/corrupt writes) are skipped.
func (w *WAL) readEntries() ([]port.WALEntry, error) {
	// Seek to beginning for read.
	if _, err := w.walFile.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}

	var entries []port.WALEntry

	for {
		// Read length prefix.
		var lenBuf [4]byte
		if _, err := io.ReadFull(w.walFile, lenBuf[:]); err != nil {
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				break // end of file or truncated entry
			}
			return nil, err
		}
		payloadLen := binary.BigEndian.Uint32(lenBuf[:])

		// Sanity check: reject absurdly large payloads.
		if payloadLen > 100*1024*1024 { // 100 MB
			break // corrupt
		}

		// Read payload.
		payload := make([]byte, payloadLen)
		if _, err := io.ReadFull(w.walFile, payload); err != nil {
			break // truncated entry — skip
		}

		// Read CRC32.
		var crcBuf [4]byte
		if _, err := io.ReadFull(w.walFile, crcBuf[:]); err != nil {
			break // truncated entry — skip
		}

		// Verify CRC32.
		expected := binary.BigEndian.Uint32(crcBuf[:])
		actual := crc32.ChecksumIEEE(payload)
		if expected != actual {
			continue // corrupt entry — skip, try next
		}

		// Unmarshal.
		var entry port.WALEntry
		if err := json.Unmarshal(payload, &entry); err != nil {
			continue // corrupt JSON — skip
		}

		entries = append(entries, entry)
	}

	return entries, nil
}
