package main

import (
	"math/rand"
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
	ACKs      []bool // hop-by-hop ACK: true если hop подтвердил получение
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
	// Прямое соединение
	if w.HasConnection(from, to) {
		return []string{from.PeerID(), to.PeerID()}
	}

	// Кэшируем соседей для оптимизации (O(n*k) вместо O(n²))
	neighbors := make(map[string][]*Node)
	for _, n := range online {
		if n.PeerID() == from.PeerID() || n.PeerID() == to.PeerID() { continue }
		var conns []*Node
		for _, other := range online {
			if other.PeerID() == n.PeerID() { continue }
			if w.HasConnection(n, other) {
				conns = append(conns, other)
			}
		}
		neighbors[n.PeerID()] = conns
	}

	// 1 relay: ищем узел, который соединяет from и to
	fromNeighbors := neighbors[from.PeerID()]
	toNeighbors := make(map[string]bool)
	for _, n := range neighbors[to.PeerID()] {
		toNeighbors[n.PeerID()] = true
	}
	
	var relay1Candidates []*Node
	for _, n := range fromNeighbors {
		if toNeighbors[n.PeerID()] {
			relay1Candidates = append(relay1Candidates, n)
		}
	}
	if len(relay1Candidates) > 0 {
		relay := relay1Candidates[rand.Intn(len(relay1Candidates))]
		return []string{from.PeerID(), relay.PeerID(), to.PeerID()}
	}

	// 2 relays: ищем пару, которая соединяет from → r1 → r2 → to
	if w.Config.DefaultHops >= 3 {
		type relayPair struct { r1, r2 *Node }
		var pairs []relayPair
		for _, r1 := range fromNeighbors {
			r1Neighbors := neighbors[r1.PeerID()]
			for _, r2 := range r1Neighbors {
				if r2.PeerID() == from.PeerID() || r2.PeerID() == to.PeerID() { continue }
				// Проверяем что r2 соединён с to
				r2Neighbors := neighbors[r2.PeerID()]
				for _, r2n := range r2Neighbors {
					if r2n.PeerID() == to.PeerID() {
						pairs = append(pairs, relayPair{r1, r2})
						break
					}
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

	// Фаза 1: обновление узлов (последовательно, без race condition)
	for s := 0; s < n; s++ {
		for _, nd := range all {
			if nd.Status != StatusOnline { continue }
			nd.Tick()
		}
	}

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

		// Доставка с hop-by-hop ACK
		w.msgMu.Lock()
		rem := w.ActiveMessages[:0]
		for i := range w.ActiveMessages {
			msg := &w.ActiveMessages[i]
			msg.Remaining--
			
			// Hop-by-hop ACK: отмечаем пройденные hop'и
			if msg.Remaining < msg.Total {
				hopIndex := len(msg.Path) - 2 - (msg.Remaining * (len(msg.Path)-1) / msg.Total)
				if hopIndex >= 0 && hopIndex < len(msg.ACKs) {
					msg.ACKs[hopIndex] = true
				}
			}
			
			if msg.Remaining <= 0 {
				// Финальная доставка
				for _, nd := range all {
					if nd.PeerID() == msg.To {
						nd.ReceivedCount++
						if msg.MsgType == "file" { nd.FileReceived++ }
						break
					}
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

		// Трансферы: user-initiated (каждые ~10 минут, 10% шанс)
		if t%6000 == 0 {
			for _, nd := range online {
				if nd.Balance > 50 && rand.Float64() < 0.1 {
					// Находим случайного получателя с низким балансом
					var target *Node
					for i := 0; i < 10; i++ {
						tg := online[rand.Intn(len(online))]
						if tg.ID != nd.ID && tg.Balance < 10 {
							target = tg
							break
						}
					}
					if target == nil { continue }
					
					amount := nd.Balance * (0.1 + rand.Float64()*0.2) // 10-30% от баланса
					if amount < 1 { continue }
					
					// Проверяем что есть путь
					path := w.findPath(nd, target, online)
					if len(path) < 2 { continue }
					
					nd.Balance -= amount
					target.Balance += amount
					atomic.AddInt64(&w.TotalTransfers, 1)
				}
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
	return &ActiveMsg{
		ID:        from.PeerID()[:8] + "-" + to.PeerID()[:8],
		From:      from.PeerID(),
		To:        to.PeerID(),
		Path:      path,
		Remaining: ticks,
		Total:     ticks,
		Size:      mb,
		MsgType:   mts,
		ACKs:      make([]bool, len(path)-1), // hop-by-hop ACK
	}
}

// findRandomRelay находит случайный промежуточный relay (не прямой)
func (w *World) findRandomRelay(from, to *Node, online []*Node, excludePath []string) *Node {
	exclude := make(map[string]bool)
	for _, pid := range excludePath { exclude[pid] = true }
	
	// Ищем узлы, которые НЕ соединены напрямую ни с from, ни с to
	// но могут быть промежуточными relay
	var candidates []*Node
	for _, n := range online {
		if exclude[n.PeerID()] { continue }
		if n.PeerID() == from.PeerID() || n.PeerID() == to.PeerID() { continue }
		
		// Проверяем что узел НЕ соединён напрямую ни с from, ни с to
		// (иначе это будет прямой путь, а не relay)
		if w.HasConnection(from, n) && w.HasConnection(n, to) {
			continue // это прямой путь, пропускаем
		}
		
		// Узел должен быть соединён хотя бы с одним из них
		if w.HasConnection(from, n) || w.HasConnection(n, to) {
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

// SendMulticast отправляет групповое сообщение через multicast-дерево
func (w *World) SendMulticast(from *Node, recipients []*Node, msgBytes int, online []*Node) []*ActiveMsg {
	if from.Status != StatusOnline || len(recipients) == 0 { return nil }
	
	// Строим минимальное покрывающее дерево (MST) от from ко всем recipients
	// Упрощённо: отправляем отдельное сообщение каждому recipient через findPath
	var msgs []*ActiveMsg
	for _, to := range recipients {
		if to.PeerID() == from.PeerID() { continue }
		m := w.sendMsg(from, to, MsgMulticast, msgBytes, online, w.AllNodes())
		if m != nil {
			atomic.AddInt64(&w.TotalSent, 1)
			from.SentCount++
			from.LastSendTick = w.Tick
			w.msgMu.Lock()
			w.ActiveMessages = append(w.ActiveMessages, *m)
			w.msgMu.Unlock()
			msgs = append(msgs, m)
		}
	}
	return msgs
}

// SendFirmwareUpdate отправляет обновление прошивки через mesh
func (w *World) SendFirmwareUpdate(from *Node, target *Node, firmwareBytes int, online []*Node) *ActiveMsg {
	if from.Status != StatusOnline || target.Status != StatusOnline { return nil }
	
	// Firmware updates всегда идут через onion (минимум 1 hop)
	m := w.sendMsg(from, target, MsgFirmware, firmwareBytes, online, w.AllNodes())
	if m != nil {
		atomic.AddInt64(&w.TotalSent, 1)
		from.SentCount++
		from.LastSendTick = w.Tick
		from.FileSent++
		w.msgMu.Lock()
		w.ActiveMessages = append(w.ActiveMessages, *m)
		w.msgMu.Unlock()
	}
	return m
}
