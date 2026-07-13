package main

import (
	"math"
	"math/rand"
	"runtime"
	"sync"
	"sync/atomic"
)

type World struct {
	mu      sync.RWMutex
	msgMu   sync.Mutex
	Config  Config
	Nodes   [][]*Node
	Tick    int
	Speed   int
	TPS     int

	TotalSent     int64
	TotalReceived int64
	TotalFailed   int64
	TotalRelayed  int64
	TotalRELAY    int64
	TotalTransfers int64

	ActiveMessages []ActiveMsg
	Events         []Event
}

type ActiveMsg struct {
	ID        string
	From      string
	To        string
	Path      []string
	Remaining int
	Total     int
	Size      int
	MsgType   string
}

type Event struct {
	Tick int
	Type string
	From string
	To   string
	Msg  string
}

func NewWorld(cfg Config) *World {
	w := &World{Config: cfg, Speed: 10}
	w.Nodes = make([][]*Node, cfg.GridHeight)
	for y := 0; y < cfg.GridHeight; y++ {
		w.Nodes[y] = make([]*Node, cfg.GridWidth)
		for x := 0; x < cfg.GridWidth; x++ {
			w.Nodes[y][x] = NewNode(x, y, &cfg)
		}
	}
	return w
}

func (w *World) NodeAt(x, y int) *Node {
	if y < 0 || y >= len(w.Nodes) || x < 0 || x >= len(w.Nodes[y]) { return nil }
	return w.Nodes[y][x]
}

func (w *World) AllNodes() []*Node {
	nodes := make([]*Node, 0, w.Config.GridHeight*w.Config.GridWidth)
	for y := 0; y < w.Config.GridHeight; y++ {
		for x := 0; x < w.Config.GridWidth; x++ {
			nodes = append(nodes, w.Nodes[y][x])
		}
	}
	return nodes
}

func (w *World) OnlineNodes() []*Node {
	nodes := make([]*Node, 0, len(w.Nodes)*len(w.Nodes[0]))
	for y := 0; y < w.Config.GridHeight; y++ {
		for x := 0; x < w.Config.GridWidth; x++ {
			if w.Nodes[y][x].Status == StatusOnline {
				nodes = append(nodes, w.Nodes[y][x])
			}
		}
	}
	return nodes
}

func (w *World) IsJammed(x, y int) bool {
	if !w.Config.JammingEnabled { return false }
	for _, j := range w.Config.JammingCells {
		if j[0] == x && j[1] == y { return true }
	}
	return false
}

func (w *World) HasConnection(a, b *Node) bool {
	if a.Status != StatusOnline || b.Status != StatusOnline { return false }
	if w.IsJammed(a.X, a.Y) || w.IsJammed(b.X, b.Y) { return false }
	dist := a.DistanceTo(b, w.Config.CellWidth, w.Config.CellHeight)
	walls, floors := a.WallsBetween(b)
	if dist <= w.Config.WiFiRange {
		snr := NewWiFiRadio().SNR(dist, walls, floors, w.Config.WallAtten, w.Config.FloorAtten, w.Config.NoiseFloor)
		if PacketSuccessRate(snr) > 0.1 { return true }
	}
	if dist <= w.Config.LoRaRange { return true }
	return false
}

func (w *World) findPath(from, to *Node, online []*Node) []string {
	// Прямое соединение (но мы всё равно добавим relay позже)
	if w.HasConnection(from, to) {
		return []string{from.PeerID(), to.PeerID()}
	}

	// 1 relay: собираем ВСЕХ кандидатов, выбираем случайного
	var relay1Candidates []*Node
	for _, n := range online {
		if n.PeerID() == from.PeerID() || n.PeerID() == to.PeerID() { continue }
		if w.HasConnection(from, n) && w.HasConnection(n, to) {
			relay1Candidates = append(relay1Candidates, n)
		}
	}
	if len(relay1Candidates) > 0 {
		relay := relay1Candidates[rand.Intn(len(relay1Candidates))]
		return []string{from.PeerID(), relay.PeerID(), to.PeerID()}
	}

	// 2 relays: собираем ВСЕХ кандидатов, выбираем случайную пару
	if w.Config.DefaultHops >= 3 {
		type relayPair struct { r1, r2 *Node }
		var pairs []relayPair
		for _, r1 := range online {
			if r1.PeerID() == from.PeerID() || r1.PeerID() == to.PeerID() || !w.HasConnection(from, r1) { continue }
			for _, r2 := range online {
				if r2.PeerID() == r1.PeerID() || r2.PeerID() == from.PeerID() || r2.PeerID() == to.PeerID() { continue }
				if w.HasConnection(r1, r2) && w.HasConnection(r2, to) {
					pairs = append(pairs, relayPair{r1, r2})
				}
			}
		}
		if len(pairs) > 0 {
			pair := pairs[rand.Intn(len(pairs))]
			return []string{from.PeerID(), pair.r1.PeerID(), pair.r2.PeerID(), to.PeerID()}
		}
	}
	return nil
}

func (w *World) deliveryTicks(from, to *Node, msgBytes, relayCount int) int {
	dist := from.DistanceTo(to, w.Config.CellWidth, w.Config.CellHeight)
	walls, floors := from.WallsBetween(to)
	route := "lora"
	if dist <= w.Config.WiFiRange {
		snr := NewWiFiRadio().SNR(dist, walls, floors, w.Config.WallAtten, w.Config.FloorAtten, w.Config.NoiseFloor)
		if PacketSuccessRate(snr) > 0.1 { route = "wifi" }
	}
	if route == "wifi" {
		ticks := int(float64(w.Config.DefaultHops) * 0.5 / SecondsPerTick)
		if ticks < 1 { ticks = 1 }
		return ticks * (relayCount + 1)
	}
	return from.DeliveryTicks(msgBytes, NewLoRaRadio(), dist, walls, floors, w.Config.WallAtten, w.Config.FloorAtten, w.Config.NoiseFloor) * (relayCount + 1)
}

func (w *World) RunSteps(n int) {
	if n < 1 { n = 1 }
	all := w.AllNodes()
	online := w.OnlineNodes()
	nCPU := runtime.NumCPU()
	chunk := (len(all) + nCPU - 1) / nCPU

	// Фаза 1: обновление узлов (параллельно)
	var wg sync.WaitGroup
	for i := 0; i < nCPU; i++ {
		start := i * chunk
		end := start + chunk
		if end > len(all) { end = len(all) }
		if start >= end { continue }
		wg.Add(1)
		go func(nodes []*Node, steps int) {
			defer wg.Done()
			for s := 0; s < steps; s++ {
				for _, nd := range nodes {
					if nd.Status != StatusOnline { continue }
					nd.Uptime += SecondsPerTick / 3600.0
					nd.EWMAWork *= math.Exp(-SecondsPerTick * math.Ln2 / 604800.0)
					if nd.StorageBytes > 0 {
						rw := nd.StorageBytes / (1024 * 1024) * nd.cfg.StorageReward * SecondsPerTick / 3600.0
						nd.Balance += rw
						nd.EWMAWork += rw * 0.01
						nd.StorageHours += SecondsPerTick / 3600.0
					}
				}
			}
		}(all[start:end], n)
	}
	wg.Wait()

	// Фаза 2: обработка шагов (однопоточно)
	for s := 0; s < n; s++ {
		w.Tick++
		t := w.Tick

		// Статусы и репутация
		for _, nd := range all {
			if t%300 == 0 { nd.UpdateReputation() }
			if nd.ShouldChangeStatus(t) {
				if nd.Status == StatusOnline {
					nd.Status = StatusOffline
					nd.OfflineSince = t
				} else {
					nd.Status = StatusOnline
					nd.OfflineSince = -1
					nd.scheduleNextSend(t)
				}
				nd.scheduleNextStatus(t)
			}
		}

		// Доставка
		w.msgMu.Lock()
		rem := w.ActiveMessages[:0]
		for i := range w.ActiveMessages {
			msg := &w.ActiveMessages[i]
			msg.Remaining--
			if msg.Remaining <= 0 {
				for _, nd := range all {
					if nd.PeerID() == msg.To { nd.ReceivedCount++; if msg.MsgType == "file" { nd.FileReceived++ }; break }
				}
				atomic.AddInt64(&w.TotalReceived, 1)
			} else {
				rem = append(rem, *msg)
			}
		}
		w.ActiveMessages = rem
		w.msgMu.Unlock()

		// Обновляем online список если были изменения статусов
		online = w.OnlineNodes()

		// Трафик
		for _, nd := range online {
			if !nd.ShouldSend(t) { continue }
			var target *Node
			for i := 0; i < 20; i++ {
				tg := online[rand.Intn(len(online))]
				if tg.ID != nd.ID && len(w.findPath(nd, tg, online)) >= 2 { target = tg; break }
			}
			if target == nil { nd.scheduleNextSend(t); continue }

			msgType := MsgText
			msgSize := 100 + rand.Intn(400)
			if rand.Float64() < 0.15 {
				fs := 10000 + rand.Intn(40000)
				rc := len(w.findPath(nd, target, online)) - 2
				if rc < 0 { rc = 0 }
				cost := float64(rc*fs) / 1024.0 * nd.cfg.SendCost
				if nd.Available() >= cost || nd.Balance-cost >= -nd.CreditLimit() { msgType = MsgFile; msgSize = fs }
			}
			m := w.sendMsg(nd, target, msgType, msgSize, online, all)
			if m != nil {
				atomic.AddInt64(&w.TotalSent, 1)
				nd.SentCount++
				nd.LastSendTick = t
				if msgType == MsgFile { nd.FileSent++ }
				w.msgMu.Lock()
				w.ActiveMessages = append(w.ActiveMessages, *m)
				w.msgMu.Unlock()
			}
			nd.scheduleNextSend(t)
		}

		// Трансферы: зелёные → красным (каждые ~5 минут)
		if t%3000 == 0 {
			var greens, reds []*Node
			for _, nd := range online {
				if nd.Balance > 50 {
					greens = append(greens, nd)
				} else if nd.Balance < -10 {
					reds = append(reds, nd)
				}
			}
			// Каждый зелёный с 30% шансом отправляет часть избытка случайному красному
			for _, g := range greens {
				if len(reds) == 0 || rand.Float64() > 0.3 { continue }
				r := reds[rand.Intn(len(reds))]
				amount := g.Balance * (0.1 + rand.Float64()*0.2) // 10-30% от баланса
				if amount < 1 { continue }
				g.Balance -= amount
				r.Balance += amount
				atomic.AddInt64(&w.TotalTransfers, 1)
				// Упрощённо: прямой перевод, без relay-комиссии
				// (в реальности пошёл бы через sendMsg с MsgTransfer)
			}
		}
	}
}

func (w *World) sendMsg(from, to *Node, mt MsgType, mb int, online, all []*Node) *ActiveMsg {
	if from.Status != StatusOnline || to.Status != StatusOnline { return nil }
	path := w.findPath(from, to, online)
	if len(path) < 2 { return nil }
	rc := len(path) - 2
	if rc < 0 { rc = 0 }
	
	// Обязательный relay: минимум MinRelayHops
	if rc < w.Config.MinRelayHops {
		// Принудительно добавляем relay
		for i := 0; i < w.Config.MinRelayHops - rc; i++ {
			relay := w.findRandomRelay(from, to, online, path)
			if relay != nil {
				// Вставляем relay в path перед to
				newPath := make([]string, len(path)+1)
				copy(newPath, path[:len(path)-1])
				newPath[len(path)-1] = relay.PeerID()
				newPath[len(path)] = to.PeerID()
				path = newPath
			}
		}
		rc = len(path) - 2
		if rc < 0 { rc = 0 }
	}
	
	ch := rc
	if ch < 1 { ch = 1 }
	cn := from.ConfirmNRequired(mb, ch)
	if cn < 0 {
		cost := float64(rc*mb) / 1024.0 * from.cfg.SendCost
		if cost > 0 && from.Available() < cost && from.Balance-cost < -from.CreditLimit() { return nil }
		if cost > 0 {
			burn := cost * from.cfg.BurnRate
			from.Balance -= cost
			atomic.AddInt64(&w.TotalRELAY, int64((cost-burn)*100))
		}
	}
	if cn > 0 {
		// Эмиссия только если relay-очередь не пуста (упрощённо: 80% шанс)
		if rand.Float64() < 0.8 {
			emission := float64(cn*from.cfg.RelayChunkSize) / 1024.0 * from.cfg.EmissionRate
			from.Balance += emission
		}
		from.EWMAWork += float64(cn*from.cfg.RelayChunkSize)
		atomic.AddInt64(&w.TotalRelayed, int64(cn))
	}
	ticks := w.deliveryTicks(from, to, mb, rc)
	if ticks < 1 { ticks = 1 }
	if ticks > 3000 { ticks = 3000 }
	mts := "text"
	if mt == MsgFile { mts = "file" }
	for i := 1; i < len(path)-1; i++ {
		for _, nd := range all {
			if nd.PeerID() == path[i] { nd.AddRelayWork(float64(mb)); break }
		}
	}
	return &ActiveMsg{ID: from.PeerID()[:8] + "-" + to.PeerID()[:8], From: from.PeerID(), To: to.PeerID(), Path: path, Remaining: ticks, Total: ticks, Size: mb, MsgType: mts}
}

// findRandomRelay находит случайный relay из online, который может соединить from и to
func (w *World) findRandomRelay(from, to *Node, online []*Node, excludePath []string) *Node {
	exclude := make(map[string]bool)
	for _, pid := range excludePath { exclude[pid] = true }
	
	var candidates []*Node
	for _, n := range online {
		if exclude[n.PeerID()] { continue }
		if w.HasConnection(from, n) && w.HasConnection(n, to) {
			candidates = append(candidates, n)
		}
	}
	
	if len(candidates) == 0 { return nil }
	return candidates[rand.Intn(len(candidates))]
}

func (w *World) ToggleNode(x, y int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	n := w.NodeAt(x, y)
	if n == nil { return }
	if n.Status == StatusOnline {
		n.Status = StatusOffline
		n.OfflineSince = w.Tick
	} else {
		n.Status = StatusOnline
		n.OfflineSince = -1
		n.scheduleNextSend(w.Tick)
		n.scheduleNextStatus(w.Tick)
	}
}

func (w *World) CountByBalance(min, max float64) int {
	c := 0
	for y := 0; y < w.Config.GridHeight; y++ {
		for x := 0; x < w.Config.GridWidth; x++ {
			b := w.Nodes[y][x].Balance
			if b >= min && b < max { c++ }
		}
	}
	return c
}

func (w *World) TotalSupply() float64 {
	sum := 0.0
	for y := 0; y < w.Config.GridHeight; y++ {
		for x := 0; x < w.Config.GridWidth; x++ {
			if w.Nodes[y][x].Balance > 0 {
				sum += w.Nodes[y][x].Balance
			}
		}
	}
	return sum
}
