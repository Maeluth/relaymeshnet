package relay

import (
	"crypto/sha256"
	"errors"
	"sync"
	"time"
)

type Priority uint8

const (
	PriorityHigh   Priority = 0
	PriorityNormal Priority = 1
	PriorityLow    Priority = 2
)

type Packet struct {
	ID        [32]byte
	From      string
	To        string
	Data      []byte
	Priority  Priority
	CreatedAt time.Time
	ACKs      []bool
}

type Queue struct {
	mu       sync.Mutex
	packets  []*Packet
	maxSize  int
}

func NewQueue(maxSize int) *Queue {
	return &Queue{
		packets: make([]*Packet, 0, maxSize),
		maxSize: maxSize,
	}
}

func (q *Queue) Push(pkt *Packet) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	if len(q.packets) >= q.maxSize {
		return ErrQueueFull
	}

	pkt.ID = sha256.Sum256(pkt.Data)
	pkt.CreatedAt = time.Now()

	// Insert in priority order
	inserted := false
	for i, existing := range q.packets {
		if pkt.Priority < existing.Priority {
			q.packets = append(q.packets[:i+1], q.packets[i:]...)
			q.packets[i] = pkt
			inserted = true
			break
		}
	}

	if !inserted {
		q.packets = append(q.packets, pkt)
	}

	return nil
}

func (q *Queue) Pop() *Packet {
	q.mu.Lock()
	defer q.mu.Unlock()

	if len(q.packets) == 0 {
		return nil
	}

	pkt := q.packets[0]
	q.packets = q.packets[1:]
	return pkt
}

func (q *Queue) Len() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.packets)
}

func (q *Queue) IsEmpty() bool {
	return q.Len() == 0
}

type HopTracker struct {
	mu   sync.Mutex
	hops map[[32]byte]*HopState
}

type HopState struct {
	PacketID  [32]byte
	HopIndex  int
	TotalHops int
	ACKed     bool
	ACKedAt   time.Time
}

func NewHopTracker() *HopTracker {
	return &HopTracker{
		hops: make(map[[32]byte]*HopState),
	}
}

func (ht *HopTracker) Track(packetID [32]byte, hopIndex, totalHops int) {
	ht.mu.Lock()
	defer ht.mu.Unlock()

	ht.hops[packetID] = &HopState{
		PacketID:  packetID,
		HopIndex:  hopIndex,
		TotalHops: totalHops,
		ACKed:     false,
	}
}

func (ht *HopTracker) ACK(packetID [32]byte) bool {
	ht.mu.Lock()
	defer ht.mu.Unlock()

	state, ok := ht.hops[packetID]
	if !ok {
		return false
	}

	state.ACKed = true
	state.ACKedAt = time.Now()
	return true
}

func (ht *HopTracker) IsACKed(packetID [32]byte) bool {
	ht.mu.Lock()
	defer ht.mu.Unlock()

	state, ok := ht.hops[packetID]
	if !ok {
		return false
	}

	return state.ACKed
}

func (ht *HopTracker) Cleanup(olderThan time.Duration) int {
	ht.mu.Lock()
	defer ht.mu.Unlock()

	cutoff := time.Now().Add(-olderThan)
	removed := 0

	for id, state := range ht.hops {
		if state.ACKed && state.ACKedAt.Before(cutoff) {
			delete(ht.hops, id)
			removed++
		}
	}

	return removed
}

var ErrQueueFull = errors.New("relay queue full")
