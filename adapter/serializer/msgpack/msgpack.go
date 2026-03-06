// Package msgpack implements [port.SerializerPort] using MessagePack encoding.
//
// MessagePack produces smaller payloads than JSON at the cost of human
// readability. Use this serializer for high-throughput workloads where
// storage efficiency matters.
//
// This package is distributed as a separate Go sub-module to avoid pulling
// the msgpack dependency into projects that don't need it.
//
//	import "github.com/abhipray-cpu/go-audit/adapter/serializer/msgpack"
//
//	serializer := msgpack.New()
//	auditor, _ := audit.New(audit.Config{Serializer: serializer, ...})
package msgpack

import (
	"context"
	"fmt"

	"github.com/abhipray-cpu/go-audit/domain/port"
	mp "github.com/vmihailenco/msgpack/v5"
)

// Compile-time check.
var _ port.SerializerPort = (*Serializer)(nil)

// Serializer implements [port.SerializerPort] with MessagePack.
type Serializer struct{}

// New creates a MsgPack [Serializer].
func New() *Serializer { return &Serializer{} }

// Marshal encodes an entity to MessagePack bytes.
func (s *Serializer) Marshal(_ context.Context, v any) ([]byte, error) {
	data, err := mp.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("msgpack marshal: %w", err)
	}
	return data, nil
}

// Unmarshal decodes MessagePack bytes into dst.
func (s *Serializer) Unmarshal(_ context.Context, data []byte, dst any) error {
	if err := mp.Unmarshal(data, dst); err != nil {
		return fmt.Errorf("msgpack unmarshal: %w", err)
	}
	return nil
}

// ContentType returns the MIME type for MessagePack.
func (s *Serializer) ContentType() string {
	return "application/msgpack"
}
