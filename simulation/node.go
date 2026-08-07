package main

import (
	"crypto/sha256"
	"fmt"
	"math"
	"math/rand"
	"sync"
)

type NodeStatus int

const (
	StatusOnline  NodeStatus = iota
	StatusOffline
)

type BehaviorProfile int

const (
	ProfileChatter  BehaviorProfile = iota
	ProfileNormal
	ProfileLurker
	ProfileRelayOnly
	ProfileUnstable
)

type MsgType int

const (
	MsgText MsgType = iota
	MsgFile
	MsgMulticast // групповое сообщение
	MsgFirmware  // обновление прошивки
)

type Node struct {
	ID     string
	X      int
	Y      int
	Name   string
	Status NodeStatus
	Profile BehaviorProfile

	Uptime     float64
	TickBorn   int
	LastSendTick  int
	OfflineSince  int // тик ухода в офлайн, -1 если онлайн

	Balance    float64
	LockedOut  float64
	Reputation float64
	EWMAWork   float64

	RelayBytesIn  float64
	RelayBytesOut float64
	StorageBytes  float64
	StorageHours  float64

	SentCount     int
	ReceivedCount int
	FailedCount   int
	FileSent      int
	FileReceived  int
	CollidedCount int

	nextSendTick   int
	nextStatusTick int

	mu  sync.Mutex
	cfg *Config
}

func NewNode(x, y int, cfg *Config) *Node {
	id := fmt.Sprintf("node-%d-%d-%x", x, y, rand.Intn(0xFFFF))
	profile := randomProfile()
	n := &Node{
		ID:           id,
		X:            x,
		Y:            y,
		Name:         fmt.Sprintf("Кв.%d эт.%d", x+1, y+1),
		Status:       StatusOnline,
		Profile:      profile,
		TickBorn:     0,
		LastSendTick: -9999,
		OfflineSince: -1,
		cfg:          cfg,
	}
	n.scheduleNextSend(0)
	n.scheduleNextStatus(0)
	if rand.Float64() < 0.05 {
		n.Status = StatusOffline
	}
	return n
}

func randomProfile() BehaviorProfile {
	r := rand.Float64()
	switch {
	case r < 0.10:
		return ProfileChatter
	case r < 0.55:
		return ProfileNormal
	case r < 0.75:
		return ProfileLurker
	case r < 0.85:
		return ProfileRelayOnly
	default:
		return ProfileUnstable
	}
}

func (n *Node) scheduleNextSend(currentTick int) {
	minTicks, maxTicks := 500, 2500 // 1-5 минут
	switch n.Profile {
	case ProfileChatter:
		minTicks, maxTicks = 250, 1000 // 30 сек - 2 мин
	case ProfileLurker:
		minTicks, maxTicks = 2500, 15000 // 5-30 мин
	case ProfileRelayOnly:
		minTicks, maxTicks = 99999999, 99999999
	case ProfileUnstable:
		minTicks, maxTicks = 500, 5000 // 1-10 мин
	}
	// Первый раз — небольшой разброс чтобы не все одновременно
	firstDelay := rand.Intn(300) + 50 // 50-350 тиков (~6-42 сек)
	if currentTick > 0 {
		firstDelay = 0
	}
	n.nextSendTick = currentTick + firstDelay + minTicks + rand.Intn(maxTicks-minTicks+1)
}

func (n *Node) scheduleNextStatus(currentTick int) {
	if n.Profile != ProfileUnstable {
		n.nextStatusTick = currentTick + 3600*500 // 1 час
		return
	}
	// unstable: 2-15 минут
	minT := int(120.0 / SecondsPerTick)
	maxT := int(900.0 / SecondsPerTick)
	n.nextStatusTick = currentTick + minT + rand.Intn(maxT-minT+1)
}

func (n *Node) PeerID() string {
	h := sha256.Sum256([]byte(n.ID))
	return fmt.Sprintf("%x", h[:16])
}

func (n *Node) Available() float64 { return n.Balance - n.LockedOut }

func (n *Node) CreditLimit() float64 {
	return 20.0 * math.Sqrt(math.Max(n.Reputation, 0.01))
}

func (n *Node) EffectiveSF(snr float64) LoRaSF {
	if snr > 5 { return SF7 }
	if snr > -5 { return SF9 }
	return SF12
}

func (n *Node) OnionPacketSize(messageBytes int) int { return messageBytes + 185 }

func (n *Node) FragmentsFor(messageBytes int, radio RadioModel, dist float64, walls, floors int, wa, fa, nf float64) (int, float64, LoRaSF) {
	snr := radio.SNR(dist, walls, floors, wa, fa, nf)
	sf := n.EffectiveSF(snr)
	pkt := n.OnionPacketSize(messageBytes)
	frags := FragmentsNeeded(pkt, sf)
	// Air time для одного фрагмента (MaxPayload), а не для всего пакета
	fragSize := sf.MaxPayload()
	if pkt < fragSize { fragSize = pkt }
	air := sf.AirTime(fragSize)
	return frags, air, sf
}

func (n *Node) DeliveryTicks(messageBytes int, radio RadioModel, dist float64, walls, floors int, wa, fa, nf float64) int {
	frags, air, _ := n.FragmentsFor(messageBytes, radio, dist, walls, floors, wa, fa, nf)
	totalAir := air * float64(frags)
	dcDelay := DutyCycleDelay(totalAir, 0.01)
	ticks := int(dcDelay / SecondsPerTick)
	if ticks < 1 { ticks = 1 }
	if ticks > 3000 { ticks = 3000 } // cap ~6 "минут"
	return ticks
}

func (n *Node) ConfirmNRequired(msgBytes, hops int) int {
	if msgBytes > n.cfg.ConfirmThreshold { return -1 }
	N := int(math.Ceil(float64(hops*msgBytes) / float64(n.cfg.RelayChunkSize)))
	if N < 2 { N = 2 }
	return N
}

func (n *Node) Tick() {
	// Reputation demurrage (~1% в месяц, ~0.000022% в тик при 500 тиках/мин)
	// Применяется только к Reputation, НЕ к EWMAWork (чтобы не было двойного decay)
	repDemurrage := 0.00000022
	if n.Status != StatusOnline {
		repDemurrage *= 2.0 // ×2 для офлайн
	}
	n.Reputation -= n.Reputation * repDemurrage
	if n.Reputation < 0 {
		n.Reputation = 0
	}

	if n.Status != StatusOnline { return }
	n.Uptime += SecondsPerTick / 3600.0

	// EWMAWork decay (half-life = 7 days = 604800 seconds)
	// НЕ применяем demurrage здесь, только decay для EWMA
	decayFactor := math.Exp(-SecondsPerTick * math.Ln2 / 604800.0)
	n.EWMAWork *= decayFactor

	if n.StorageBytes > 0 {
		reward := n.StorageBytes / (1024 * 1024) * n.cfg.StorageReward * SecondsPerTick / 3600.0
		n.Balance += reward
		n.EWMAWork += reward * 0.01
		n.StorageHours += SecondsPerTick / 3600.0
	}
}

func (n *Node) AddRelayWork(bytes float64, multiplier float64) {
	n.RelayBytesOut += bytes
	reward := bytes / float64(n.cfg.RelayChunkSize) * n.cfg.RelayReward * multiplier
	burn := reward * n.cfg.BurnRate
	n.Balance += reward - burn
	n.EWMAWork += bytes
}

func (n *Node) UpdateReputation() { n.Reputation = n.EWMAWork / 168.0 }

func (n *Node) DistanceTo(other *Node, cw, ch float64) float64 {
	dx := float64(n.X-other.X) * cw
	dy := float64(n.Y-other.Y) * ch
	return math.Sqrt(dx*dx + dy*dy)
}

func (n *Node) WallsBetween(other *Node) (int, int) {
	return int(math.Abs(float64(n.X - other.X))), int(math.Abs(float64(n.Y - other.Y)))
}

func (n *Node) ShouldSend(now int) bool {
	if n.Profile == ProfileRelayOnly || n.Status != StatusOnline { return false }
	return now >= n.nextSendTick && now-n.LastSendTick >= 500 // max 1 per minute
}

func (n *Node) ShouldChangeStatus(now int) bool {
	return n.Profile == ProfileUnstable && now >= n.nextStatusTick
}
