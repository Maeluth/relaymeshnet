package economy

import (
	"math"
	"sync"
	"time"
)

const (
	RelayChunkSize   = 512
	ConfirmThreshold = 2048
	EmissionRate     = 0.5
	BurnRate         = 0.01
	RelayReward      = 1.0
	SendCost         = 1.0
	DebtLimit        = -100.0
)

type Balance struct {
	mu       sync.RWMutex
	credits  float64
	locked   float64
}

func NewBalance() *Balance {
	return &Balance{
		credits: 0,
		locked:  0,
	}
}

func (b *Balance) Add(amount float64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.credits += amount
}

func (b *Balance) Subtract(amount float64) bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.credits-amount < DebtLimit {
		return false
	}

	b.credits -= amount
	return true
}

func (b *Balance) Lock(amount float64) bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.credits-b.locked < amount {
		return false
	}

	b.locked += amount
	return true
}

func (b *Balance) Unlock(amount float64) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.locked >= amount {
		b.locked -= amount
	}
}

func (b *Balance) Available() float64 {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.credits - b.locked
}

func (b *Balance) Total() float64 {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.credits
}

type Reputation struct {
	mu        sync.RWMutex
	ewmaWork  float64
	score     float64
	lastUpdate time.Time
}

func NewReputation() *Reputation {
	return &Reputation{
		ewmaWork:   0,
		score:      0,
		lastUpdate: time.Now(),
	}
}

func (r *Reputation) AddWork(bytes int64) {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(r.lastUpdate).Hours()

	decayFactor := math.Exp(-elapsed * math.Ln2 / 168.0)
	r.ewmaWork = r.ewmaWork*decayFactor + float64(bytes)
	r.lastUpdate = now

	r.score = r.ewmaWork / 168.0
}

func (r *Reputation) Score() float64 {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.score
}

func (r *Reputation) CreditLimit() float64 {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return 20.0 * math.Sqrt(math.Max(r.score, 0.01))
}

func (r *Reputation) ApplyDemurrage() {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(r.lastUpdate).Hours()

	demurrageRate := 0.01 / (30.0 * 24.0)
	demurrage := r.score * demurrageRate * elapsed

	r.score -= demurrage
	if r.score < 0 {
		r.score = 0
	}

	r.lastUpdate = now
}

type ConfirmN struct {
	relayQueue *RelayQueue
}

type RelayQueue struct {
	mu      sync.Mutex
	packets [][]byte
}

func NewRelayQueue() *RelayQueue {
	return &RelayQueue{
		packets: make([][]byte, 0),
	}
}

func (q *RelayQueue) Push(packet []byte) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.packets = append(q.packets, packet)
}

func (q *RelayQueue) Pop() ([]byte, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if len(q.packets) == 0 {
		return nil, false
	}

	packet := q.packets[0]
	q.packets = q.packets[1:]
	return packet, true
}

func (q *RelayQueue) Len() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.packets)
}

func CalculateConfirmN(msgBytes, hops int) int {
	if msgBytes > ConfirmThreshold {
		return -1
	}

	n := int(math.Ceil(float64(hops*msgBytes) / float64(RelayChunkSize)))
	if n < 2 {
		n = 2
	}
	return n
}

func CalculateEmission(n int) float64 {
	if n <= 0 {
		return 0
	}
	return float64(n) * EmissionRate
}

func CalculateSendCost(msgBytes, hops int) float64 {
	chunks := math.Ceil(float64(msgBytes) / float64(RelayChunkSize))
	return float64(hops) * chunks * SendCost
}

func CalculateRelayReward(msgBytes int, multiplier float64) float64 {
	chunks := math.Ceil(float64(msgBytes) / float64(RelayChunkSize))
	return chunks * RelayReward * multiplier
}

func CalculateBurn(cost float64) float64 {
	return cost * BurnRate
}
