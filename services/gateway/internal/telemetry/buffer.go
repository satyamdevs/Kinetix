package telemetry

import "context"

// Sink receives telemetry records. Implementations must be safe for
// concurrent use; writes happen after a request completes and must never
// block or fail the caller.
type Sink interface {
	Record(ctx context.Context, r Record) error
	Close() error
}

// Buffer is a bounded in-memory sink used when no ClickHouse is configured
// (dev) and for tests. It drops oldest records once full.
type Buffer struct {
	records []Record
	max     int
	hi      int
}

func NewBuffer(max int) *Buffer {
	if max <= 0 {
		max = 1024
	}
	return &Buffer{records: make([]Record, 0, max), max: max}
}

func (b *Buffer) Record(_ context.Context, r Record) error {
	if len(b.records) == b.max {
		// drop oldest to bound memory
		copy(b.records, b.records[1:])
		b.records = b.records[:b.max-1]
	}
	b.records = append(b.records, r)
	b.hi++
	return nil
}

// Len reports how many records are currently held.
func (b *Buffer) Len() int { return len(b.records) }

// Snapshot returns a copy of the held records in insertion order.
func (b *Buffer) Snapshot() []Record {
	out := make([]Record, len(b.records))
	copy(out, b.records)
	return out
}

func (b *Buffer) Close() error { return nil }

// Discard drops all records; a black-hole Sink for tests.
type Discard struct{}

func (Discard) Record(context.Context, Record) error { return nil }
func (Discard) Close() error                         { return nil }
