package dht

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"sync"
	"time"
)

const (
	KeySize    = 20
	BucketSize = 20
	MaxBuckets = 160
)

type Key [KeySize]byte

func KeyFromPeerID(peerID string) Key {
	hash := sha256.Sum256([]byte(peerID))
	var key Key
	copy(key[:], hash[:KeySize])
	return key
}

func KeyFromString(s string) Key {
	hash := sha256.Sum256([]byte(s))
	var key Key
	copy(key[:], hash[:KeySize])
	return key
}

func (k Key) Hex() string {
	return hex.EncodeToString(k[:])
}

func XOR(a, b Key) Key {
	var result Key
	for i := 0; i < KeySize; i++ {
		result[i] = a[i] ^ b[i]
	}
	return result
}

func (k Key) IsZero() bool {
	for _, b := range k {
		if b != 0 {
			return false
		}
	}
	return true
}

func LeadingZeros(k Key) int {
	count := 0
	for i := 0; i < KeySize; i++ {
		if k[i] == 0 {
			count += 8
		} else {
			for j := 7; j >= 0; j-- {
				if k[i]&(1<<byte(j)) == 0 {
					count++
				} else {
					return count
				}
			}
		}
	}
	return count
}

type Node struct {
	ID       Key
	PeerID   string
	Address  string
	LastSeen time.Time
}

type Bucket struct {
	nodes []*Node
}

func NewBucket() *Bucket {
	return &Bucket{
		nodes: make([]*Node, 0, BucketSize),
	}
}

func (b *Bucket) Add(node *Node) bool {
	for i, existing := range b.nodes {
		if existing.ID == node.ID {
			b.nodes[i].LastSeen = time.Now()
			return true
		}
	}

	node.LastSeen = time.Now()

	if len(b.nodes) < BucketSize {
		b.nodes = append(b.nodes, node)
		return true
	}

	// Eviction: if the oldest node hasn't been seen recently, replace it
	oldest := 0
	for i, n := range b.nodes {
		if n.LastSeen.Before(b.nodes[oldest].LastSeen) {
			oldest = i
		}
	}
	if time.Since(b.nodes[oldest].LastSeen) > 15*time.Minute {
		b.nodes[oldest] = node
		return true
	}

	return false
}

func (b *Bucket) Remove(id Key) {
	for i, node := range b.nodes {
		if node.ID == id {
			b.nodes = append(b.nodes[:i], b.nodes[i+1:]...)
			return
		}
	}
}

func (b *Bucket) Len() int {
	return len(b.nodes)
}

func (b *Bucket) Nodes() []*Node {
	result := make([]*Node, len(b.nodes))
	copy(result, b.nodes)
	return result
}

type Table struct {
	mu      sync.RWMutex
	selfID  Key
	buckets [MaxBuckets]*Bucket
}

func NewTable(selfID Key) *Table {
	t := &Table{
		selfID: selfID,
	}
	for i := 0; i < MaxBuckets; i++ {
		t.buckets[i] = NewBucket()
	}
	return t
}

func (t *Table) Add(node *Node) bool {
	if node.ID == t.selfID {
		return false
	}

	distance := XOR(t.selfID, node.ID)
	bucketIndex := LeadingZeros(distance)
	if bucketIndex >= MaxBuckets {
		bucketIndex = MaxBuckets - 1
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	return t.buckets[bucketIndex].Add(node)
}

func (t *Table) Remove(id Key) {
	distance := XOR(t.selfID, id)
	bucketIndex := LeadingZeros(distance)
	if bucketIndex >= MaxBuckets {
		bucketIndex = MaxBuckets - 1
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	t.buckets[bucketIndex].Remove(id)
}

// Closest returns up to count nodes sorted by XOR distance to target (ascending).
func (t *Table) Closest(target Key, count int) []*Node {
	t.mu.RLock()
	defer t.mu.RUnlock()

	type nodeDist struct {
		node *Node
		dist [KeySize]byte
	}

	var all []nodeDist
	for _, bucket := range t.buckets {
		for _, node := range bucket.Nodes() {
			d := XOR(target, node.ID)
			all = append(all, nodeDist{node: node, dist: d})
		}
	}

	sort.Slice(all, func(i, j int) bool {
		a, b := all[i].dist, all[j].dist
		for k := 0; k < KeySize; k++ {
			if a[k] != b[k] {
				return a[k] < b[k]
			}
		}
		return false
	})

	if len(all) > count {
		all = all[:count]
	}

	result := make([]*Node, len(all))
	for i, nd := range all {
		result[i] = nd.node
	}

	return result
}

func (t *Table) Count() int {
	t.mu.RLock()
	defer t.mu.RUnlock()

	count := 0
	for _, bucket := range t.buckets {
		count += bucket.Len()
	}
	return count
}
