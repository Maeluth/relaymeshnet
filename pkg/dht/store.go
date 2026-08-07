package dht

import (
	"sync"
	"time"
)

type Value struct {
	Data      []byte
	Signature []byte
	TTL       time.Duration
	StoredAt  time.Time
	ExpiresAt time.Time
}

type Store struct {
	mu     sync.RWMutex
	values map[Key]*Value
}

func NewStore() *Store {
	return &Store{
		values: make(map[Key]*Value),
	}
}

func (s *Store) Put(key Key, data []byte, signature []byte, ttl time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	s.values[key] = &Value{
		Data:      data,
		Signature: signature,
		TTL:       ttl,
		StoredAt:  now,
		ExpiresAt: now.Add(ttl),
	}
}

func (s *Store) Get(key Key) (*Value, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	val, ok := s.values[key]
	if !ok {
		return nil, false
	}

	if time.Now().After(val.ExpiresAt) {
		return nil, false
	}

	return val, true
}

func (s *Store) Delete(key Key) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.values, key)
}

func (s *Store) Cleanup() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	removed := 0

	for key, val := range s.values {
		if now.After(val.ExpiresAt) {
			delete(s.values, key)
			removed++
		}
	}

	return removed
}

func (s *Store) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.values)
}

type MessageType uint8

const (
	MsgPing      MessageType = 0x01
	MsgPong      MessageType = 0x02
	MsgStore     MessageType = 0x03
	MsgFindNode  MessageType = 0x04
	MsgFindValue MessageType = 0x05
	MsgNodes     MessageType = 0x06
	MsgValue     MessageType = 0x07
)

type Message struct {
	Type      MessageType
	SenderID  Key
	Target    Key
	Key       Key
	Value     []byte
	Signature []byte
	Nodes     []*Node
	Nonce     uint64
}

type RPC struct {
	table *Table
	store *Store
}

func NewRPC(table *Table, store *Store) *RPC {
	return &RPC{
		table: table,
		store: store,
	}
}

func (r *RPC) HandlePing(msg *Message) *Message {
	return &Message{
		Type:     MsgPong,
		SenderID: r.table.selfID,
		Nonce:    msg.Nonce,
	}
}

func (r *RPC) HandleStore(msg *Message) *Message {
	r.store.Put(msg.Key, msg.Value, msg.Signature, 24*time.Hour)

	return &Message{
		Type:     MsgPong,
		SenderID: r.table.selfID,
		Nonce:    msg.Nonce,
	}
}

func (r *RPC) HandleFindNode(msg *Message) *Message {
	nodes := r.table.Closest(msg.Target, BucketSize)

	return &Message{
		Type:     MsgNodes,
		SenderID: r.table.selfID,
		Nodes:    nodes,
		Nonce:    msg.Nonce,
	}
}

func (r *RPC) HandleFindValue(msg *Message) *Message {
	if val, ok := r.store.Get(msg.Key); ok {
		return &Message{
			Type:      MsgValue,
			SenderID:  r.table.selfID,
			Key:       msg.Key,
			Value:     val.Data,
			Signature: val.Signature,
			Nonce:     msg.Nonce,
		}
	}

	nodes := r.table.Closest(msg.Key, BucketSize)
	return &Message{
		Type:     MsgNodes,
		SenderID: r.table.selfID,
		Nodes:    nodes,
		Nonce:    msg.Nonce,
	}
}

func (r *RPC) Handle(msg *Message) *Message {
	switch msg.Type {
	case MsgPing:
		return r.HandlePing(msg)
	case MsgStore:
		return r.HandleStore(msg)
	case MsgFindNode:
		return r.HandleFindNode(msg)
	case MsgFindValue:
		return r.HandleFindValue(msg)
	default:
		return nil
	}
}
