package dht

import (
	"testing"
)

func TestXOR(t *testing.T) {
	a := KeyFromString("a")
	b := KeyFromString("b")
	xor := XOR(a, b)
	same := XOR(a, a)
	if !same.IsZero() {
		t.Error("XOR with self should be zero")
	}
	if xor.IsZero() {
		t.Error("XOR of different keys should not be zero")
	}
}

func TestLeadingZeros(t *testing.T) {
	var k Key
	k[0] = 0x80
	if n := LeadingZeros(k); n != 0 {
		t.Errorf("leading zeros: got %d, want 0", n)
	}
	var z Key
	if n := LeadingZeros(z); n != 160 {
		t.Errorf("leading zeros of zero key: got %d, want 160", n)
	}
}

func TestTableAddClosest(t *testing.T) {
	self := KeyFromPeerID("self")
	table := NewTable(self)

	a := &Node{ID: KeyFromPeerID("a"), PeerID: "a", Address: "addr-a"}
	b := &Node{ID: KeyFromPeerID("b"), PeerID: "b", Address: "addr-b"}
	c := &Node{ID: KeyFromPeerID("c"), PeerID: "c", Address: "addr-c"}

	table.Add(a)
	table.Add(b)
	table.Add(c)

	if table.Count() != 3 {
		t.Errorf("count: got %d, want 3", table.Count())
	}

	closest := table.Closest(self, 2)
	if len(closest) != 2 {
		t.Errorf("closest count: got %d, want 2", len(closest))
	}
}

func TestBucketAddEvict(t *testing.T) {
	b := NewBucket()
	// Add 20 nodes
	for i := 0; i < BucketSize; i++ {
		k := KeyFromPeerID(string(rune('a' + i%26)) + string(rune('0'+i)))
		ok := b.Add(&Node{ID: k, PeerID: k.Hex()})
		if !ok {
			t.Errorf("failed to add node %d", i)
		}
	}
	if b.Len() != BucketSize {
		t.Errorf("bucket size: got %d, want %d", b.Len(), BucketSize)
	}
	// Bucket full — should not evict recent nodes
	newNode := &Node{ID: KeyFromPeerID("overflow"), PeerID: "overflow"}
	if b.Add(newNode) {
		t.Error("should not add to full bucket with recent nodes")
	}
}

func TestStorePutGet(t *testing.T) {
	s := NewStore()
	key := KeyFromString("test-key")
	data := []byte("test-value")
	sig := []byte("signature")

	s.Put(key, data, sig, 60*1e9) // 60 seconds
	val, ok := s.Get(key)
	if !ok {
		t.Fatal("key not found")
	}
	if string(val.Data) != string(data) {
		t.Errorf("data: got %q, want %q", val.Data, data)
	}
}

func TestKeyHex(t *testing.T) {
	k := KeyFromString("test")
	hex := k.Hex()
	if len(hex) != 40 {
		t.Errorf("hex length: got %d, want 40", len(hex))
	}
}
