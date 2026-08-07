package poh

import (
	"crypto/sha256"
	"encoding/binary"
	"sync"
	"time"
)

type Generator struct {
	mu        sync.Mutex
	seed      [32]byte
	lastHash  [32]byte
	tickIndex uint64
	startTime time.Time
}

func NewGenerator(seed [32]byte) *Generator {
	return &Generator{
		seed:      seed,
		lastHash:  seed,
		tickIndex: 0,
		startTime: time.Now(),
	}
}

func (g *Generator) Tick() [32]byte {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.lastHash = sha256.Sum256(g.lastHash[:])
	g.tickIndex++
	return g.lastHash
}

func (g *Generator) TickN(n int) [32]byte {
	g.mu.Lock()
	defer g.mu.Unlock()

	for i := 0; i < n; i++ {
		g.lastHash = sha256.Sum256(g.lastHash[:])
		g.tickIndex++
	}
	return g.lastHash
}

func (g *Generator) CurrentHash() [32]byte {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.lastHash
}

func (g *Generator) CurrentTick() uint64 {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.tickIndex
}

func (g *Generator) StartTime() time.Time {
	return g.startTime
}

type Record struct {
	TickIndex uint64
	TickHash  [32]byte
	EventType string
	DataHash  [32]byte
	Signature []byte
}

func (g *Generator) RecordEvent(eventType string, dataHash [32]byte, signFunc func([]byte) []byte) *Record {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.lastHash = sha256.Sum256(g.lastHash[:])
	g.tickIndex++

	record := &Record{
		TickIndex: g.tickIndex,
		TickHash:  g.lastHash,
		EventType: eventType,
		DataHash:  dataHash,
	}

	if signFunc != nil {
		toSign := make([]byte, 8+32+32)
		binary.BigEndian.PutUint64(toSign[0:8], record.TickIndex)
		copy(toSign[8:40], record.TickHash[:])
		copy(toSign[40:72], record.DataHash[:])
		record.Signature = signFunc(toSign)
	}

	return record
}

type Checkpoint struct {
	TickIndex uint64
	TickHash  [32]byte
	Timestamp time.Time
}

func (g *Generator) CreateCheckpoint() *Checkpoint {
	g.mu.Lock()
	defer g.mu.Unlock()

	return &Checkpoint{
		TickIndex: g.tickIndex,
		TickHash:  g.lastHash,
		Timestamp: time.Now(),
	}
}

func VerifyRecord(record *Record, checkpoint *Checkpoint, verifyFunc func([]byte, []byte) bool) bool {
	if record.TickIndex <= checkpoint.TickIndex {
		return false
	}

	hash := checkpoint.TickHash
	for i := checkpoint.TickIndex + 1; i <= record.TickIndex; i++ {
		hash = sha256.Sum256(hash[:])
	}

	if hash != record.TickHash {
		return false
	}

	if verifyFunc != nil && len(record.Signature) > 0 {
		toSign := make([]byte, 8+32+32)
		binary.BigEndian.PutUint64(toSign[0:8], record.TickIndex)
		copy(toSign[8:40], record.TickHash[:])
		copy(toSign[40:72], record.DataHash[:])

		if !verifyFunc(toSign, record.Signature) {
			return false
		}
	}

	return true
}

func VerifyChain(startHash [32]byte, startTick uint64, endHash [32]byte, endTick uint64) bool {
	if endTick <= startTick {
		return false
	}

	hash := startHash
	for i := startTick + 1; i <= endTick; i++ {
		hash = sha256.Sum256(hash[:])
	}

	return hash == endHash
}
