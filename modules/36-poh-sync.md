# PoH Synchronization Between Nodes 🟡 Required

> **Required.** Каждый узел ведёт свой PoH-поток независимо. Без синхронизации нельзя установить порядок событий между узлами. Cross-reference механизм решает это без глобального времени.

## Проблема

```
Узел A: tick=57392, "я релеил пакет X"
Узел B: tick=12345, "я получил пакет X"

Вопрос: когда именно A релеил относительно B?
Без синхронизации — невозможно.
```

## Решение: Cross-Reference Points

### При встрече двух узлов

```go
type CrossRef struct {
    // Со стороны A
    A_PeerID    [16]byte
    A_Tick      uint64
    A_Hash      [32]byte  // PoH хэш в момент tick
    
    // Со стороны B (как A видит B)
    B_PeerID    [16]byte
    B_Tick      uint64
    B_Hash      [32]byte  // PoH хэш B в момент tick
    
    // Метаданные
    WallClock   uint64    // Unix timestamp (ненадёжный, для справки)
    ASignature  []byte    // Ed25519 подпись A
    BSignature  []byte    // Ed25519 подпись B (добавляется при подтверждении)
}

// Алиса встречает Боба
func (alice *Node) OnMeetBob(bob *Node) {
    // 1. Алиса записывает свою точку зрения
    ref := CrossRef{
        A_PeerID:   alice.peerID,
        A_Tick:     alice.poh.CurrentTick(),
        A_Hash:     alice.poh.CurrentHash(),
        B_PeerID:   bob.peerID,
        B_Tick:     bob.poh.CurrentTick(),
        B_Hash:     bob.poh.CurrentHash(),
        WallClock:  uint64(time.Now().Unix()),
    }
    
    // 2. Алиса подписывает
    ref.ASignature = alice.sign(ref.Bytes())
    
    // 3. Боб проверяет и добавляет свою подпись
    if bob.verifyCrossRef(ref) {
        ref.BSignature = bob.sign(ref.Bytes())
    }
    
    // 4. Оба сохраняют cross-reference
    alice.crossRefs = append(alice.crossRefs, ref)
    bob.crossRefs = append(bob.crossRefs, ref)
}
```

### Построение causal order

```go
// "Произошло ли событие E1 до E2?"
func (n *Node) HappenedBefore(e1, e2 *PoHEvent) (bool, error) {
    // Case 1: оба события на одном узле
    if e1.PeerID == e2.PeerID {
        return e1.Tick < e2.Tick, nil
    }
    
    // Case 2: прямая cross-reference
    for _, ref := range n.crossRefs {
        if ref.A_PeerID == e1.PeerID && ref.B_PeerID == e2.PeerID {
            // e1 был на тике ref.A_Tick когда e2 был на тике ref.B_Tick
            // e1.Tick <= ref.A_Tick и e2.Tick >= ref.B_Tick → e1 до e2
            if e1.Tick <= ref.A_Tick && e2.Tick >= ref.B_Tick {
                return true, nil
            }
        }
    }
    
    // Case 3: транзитивная связь через третий узел
    // A → C → B: если A happened before C и C happened before B
    for _, intermediate := range n.knownPeers {
        beforeC, _ := n.HappenedBefore(e1, lastEventOf(intermediate))
        afterC, _ := n.HappenedBefore(lastEventOf(intermediate), e2)
        if beforeC && afterC {
            return true, nil
        }
    }
    
    // Case 4: нет связи — события concurrent
    return false, nil
}
```

### Синхронизация PoH-потоков через gossip

```go
type PoHSyncMessage struct {
    PeerID     [16]byte
    Tick       uint64
    Hash       [32]byte
    CrossRefs  []CrossRef   // последние cross-references этого узла
    Timestamp  uint64
    Signature  []byte
}

// Каждые N минут узел gossip'ит свой PoH-статус
func (n *Node) GossipPoH() {
    ticker := time.NewTicker(5 * time.Minute)
    for range ticker.C {
        msg := &PoHSyncMessage{
            PeerID:    n.peerID,
            Tick:      n.poh.CurrentTick(),
            Hash:      n.poh.CurrentHash(),
            CrossRefs: n.recentCrossRefs(10),  // последние 10
            Timestamp: uint64(time.Now().Unix()),
        }
        msg.Signature = n.sign(msg.Bytes())
        
        // Отправляем 3 случайным соседям
        neighbors := n.randomNeighbors(3)
        for _, nb := range neighbors {
            n.send(nb, msg)
        }
    }
}

// При получении PoH-sync от соседа
func (n *Node) OnPoHSync(msg *PoHSyncMessage) {
    // 1. Проверяем подпись
    if !n.verify(msg.PeerID, msg.Signature, msg.Bytes()) {
        return
    }
    
    // 2. Верифицируем PoH-цепочку до msg.Tick
    if !n.verifyPoH(msg.PeerID, msg.Tick, msg.Hash) {
        n.alert("PoH verification failed for " + msg.PeerID)
        return
    }
    
    // 3. Обновляем cross-references
    for _, ref := range msg.CrossRefs {
        if !n.hasCrossRef(ref) {
            n.crossRefs = append(n.crossRefs, ref)
        }
    }
    
    // 4. Если наше время сильно расходится с соседом — корректируем
    n.adjustTimeOffset(msg)
}
```

### Коррекция временного смещения

```go
type TimeOffset struct {
    PeerID   [16]byte
    Offset   float64  // секунды (положительное = сосед впереди)
    MeasuredAt uint64
}

func (n *Node) adjustTimeOffset(msg *PoHSyncMessage) {
    // Вычисляем offset между нашими PoH-потоками
    // На основе cross-references
    
    for _, ref := range n.crossRefs {
        if ref.A_PeerID == n.peerID && ref.B_PeerID == msg.PeerID {
            // Наш tick ref.A_Tick → сосед tick ref.B_Tick
            // Оцениваем скорость тиков: ticks/sec для нас и соседа
            ourSpeed := float64(n.poh.CurrentTick()) / time.Since(n.startTime).Seconds()
            
            // Offset = разница в тиках / скорость тиков
            tickDiff := float64(ref.B_Tick)/float64(ref.A_Tick)*ourSpeed - ourSpeed
            offset := tickDiff / ourSpeed
            
            n.timeOffsets[msg.PeerID] = TimeOffset{
                PeerID:     msg.PeerID,
                Offset:     offset,
                MeasuredAt: uint64(time.Now().Unix()),
            }
            break
        }
    }
}

// Получить оценку "реального" времени события на основе PoH
func (n *Node) EstimateRealTime(peerID [16]byte, pohTick uint64) time.Time {
    // 1. Найти ближайший cross-reference с этим пиром
    for _, ref := range n.crossRefs {
        if ref.A_PeerID == n.peerID && ref.B_PeerID == peerID {
            // Линейная интерполяция: tick → wall clock
            ourTicksPerSec := float64(n.poh.CurrentTick()) / time.Since(n.startTime).Seconds()
            theirTicksPerSec := ourTicksPerSec  // предполагаем одинаковую скорость
            
            // Оцениваем время
            offset := n.timeOffsets[peerID]
            estimatedSec := float64(pohTick)/theirTicksPerSec + offset.Offset
            
            return n.startTime.Add(time.Duration(estimatedSec) * time.Second)
        }
    }
    
    return time.Time{}  // не можем оценить
}
```

### Практическое применение синхронизации

```go
// 1. Проверка "свежести" репутации
func (n *Node) IsReputationFresh(peerID [16]byte, repUpdate *ReputationUpdate) bool {
    // Оцениваем реальное время обновления
    estTime := n.EstimateRealTime(peerID, repUpdate.PoHTick)
    if estTime.IsZero() {
        return true  // не можем проверить — считаем свежим
    }
    
    // Репутация старше 24 часов?
    return time.Since(estTime) < 24*time.Hour
}

// 2. Разрешение конфликтов в DHT
func (n *Node) ResolveDHTConflict(rec1, rec2 *DHTRecord) *DHTRecord {
    // Если можем установить порядок — берём более новый
    before, err := n.HappenedBefore(
        &PoHEvent{PeerID: rec1.Publisher, Tick: rec1.UpdatedAt},
        &PoHEvent{PeerID: rec2.Publisher, Tick: rec2.UpdatedAt},
    )
    
    if err == nil && before {
        return rec2  // rec2 новее
    }
    if err == nil && !before {
        return rec1  // rec1 новее (или concurrent — берём первый)
    }
    
    // Не можем установить — берём по TTL
    if rec1.TTL > rec2.TTL {
        return rec1
    }
    return rec2
}

// 3. Доказательство "я был онлайн в момент X"
func (n *Node) ProveOnlineAt(targetTime time.Time) []byte {
    // Находим ближайший tick к целевому времени
    targetTick := uint64(time.Since(n.startTime).Seconds() * float64(n.poh.CurrentTick()) / 
        time.Since(n.startTime).Seconds())
    
    // Генерируем PoH-доказательство
    checkpoint := n.poh.NearestCheckpoint(targetTick)
    proof := n.poh.GenerateProofRange(checkpoint.Tick, targetTick)
    
    return proof
}
```

## Параметры

| Параметр | Значение | Описание |
|---|---|---|
| `POH_SYNC_INTERVAL` | 5 минут | Интервал gossip PoH-статуса |
| `MAX_CROSS_REFS` | 100 | Максимум хранимых cross-references |
| `POH_VERIFY_WINDOW` | 10000 тиков | Окно верификации PoH-цепочки |
| `TIME_OFFSET_MAX` | 3600 сек | Максимальное допустимое смещение времени |
| `POH_PROOF_SIZE` | 64 байта | Размер PoH-доказательства |
