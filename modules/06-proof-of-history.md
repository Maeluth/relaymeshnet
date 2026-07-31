# L2: Proof of History (PoH) 🟡 Required

> **Required.** Без trustless времени reputation ненадёжна, PoR-квитанции можно подделать, cold restart невозможен. Технически сеть работает, но доверие — иллюзия.

## Что такое Proof of History

Ключевая инновация Solana (Toly Yakovenko, 2018) — **Proof of History (PoH)**:
криптографические часы, создающие доказуемую временную последовательность
без необходимости синхронизации часов между узлами.

## Как работает PoH

```go
// PoH — это цепочка последовательных SHA256-хэшей
func generatePoH(seed []byte, count int) []PoHEntry {
    entries := make([]PoHEntry, count)
    hash := sha256(seed)

    for i := 0; i < count; i++ {
        // Каждый следующий хэш = SHA256(предыдущий хэш)
        hash = sha256(hash)
        entries[i] = PoHEntry{
            Index: i,
            Hash:  hash,
        }
    }
    return entries
}
```

Свойства:
- **Sequential**: каждый хэш зависит от предыдущего, невозможно распараллелить
- **Verifiable**: любой может проверить цепочку, пересчитав хэши (быстро — SHA256)
- **Timed**: на современных CPU SHA256 занимает ~400 нс → 1 000 000 хэшей ≈ 400 мс

## Почему это критично для mesh без интернета

В обычных сетях порядок событий решается через:
- NTP (нет интернета → нет NTP)
- Lamport timestamps (можно подделать)
- Блокчейн (требует глобального консенсуса → нет интернета → нет консенсуса)

**PoH даёт trustless timestamp без внешних источников**:
- Узел генерирует PoH непрерывно (фоновый процесс)
- Каждое событие (relay, сообщение, транзакция) привязывается к конкретному PoH-хэшу
- Любой другой узел может проверить: "это событие произошло после hash_N и до hash_{N+K}"
- Невозможно подделать порядок или время без пересчёта всей цепочки

## PoH-stream и события

```go
type PoHEntry struct {
    Index  uint64
    Hash   [32]byte
    Events []Event  // события, записанные в этот tick
}

type Event struct {
    Type      string    // "relay", "message", "transaction"
    DataHash  [32]byte  // хэш данных события
    Signature []byte    // подпись узла
}
```

Каждый узел ведёт свой PoH-stream:

```
Узел 0xABCD (stream_id = 0xABCD):
  hash_0       → seed = SHA256(pubkey + timestamp)
  hash_1       → [event: relay_packet_0xFFA, 1500 bytes]
  hash_2       → [no events]
  hash_3       → [event: outgoing_tx_to_0xBEEF, 100 RELAY]
  hash_4       → [event: relay_packet_0xCC2, 800 bytes]
  ...
  hash_57392   → текущий tick
```

## PoR с PoH: неоспоримое доказательство relay

```go
// Релей-узел получает пакет
func (n *Node) relayPacket(packet []byte) {
    // Выполняем relay
    nextHop := n.unwrapOnionLayer(packet)
    n.transport.Send(nextHop, packet, PriorityRelay)

    // Записываем в PoH-stream
    pohTick := n.poh.CurrentTick()
    n.poh.RecordEvent(Event{
        Type:     "relay",
        DataHash: sha256(packet),
        Signature: sign(n.privKey, sha256(packet) + pohTick),
    })

    // Следующий hop подписывает квитанцию
    receipt := RelayReceipt{
        RelayNode:    n.peerID,
        PacketHash:   sha256(packet),
        BytesRelayed: len(packet),
        PoHProof:     n.poh.GenerateProof(pohTick, pohTick+10), // доказательство времени
    }
}
```

**Проверка** другим узлом:
1. Получает receipt с PoHProof
2. Проверяет цепочку SHA256 (быстро)
3. Подтверждает: "хэш_57392 был вычислен ~400мс × 57392 ≈ 23 секунды после seed"
4. Relay действительно был выполнен в указанный момент

## Синхронизация PoH-потоков

Два узла встретились (gossip). Как объединить их истории?

### Проблема

Каждый узел ведёт свой PoH-поток независимо:
- Узел A: tick_A = 57392, hash_A = 0xABCD...
- Узел B: tick_B = 12345, hash_B = 0x1234...

У них разные скорости CPU, разные seed'ы, разные tick'и. Как установить
"событие X на узле A произошло ДО события Y на узле B"?

### Алгоритм: Cross-Reference Points

```go
// При встрече двух узлов (gossip):
func (n *Node) SyncPoHWithPeer(peer *Node) {
    // 1. Обмениваемся текущими состояниями
    myState := PoHState{
        PeerID:    n.peerID,
        Tick:      n.poh.CurrentTick(),
        Hash:      n.poh.CurrentHash(),
        Timestamp: time.Now(),  // wall clock (ненадёжный, но useful)
    }
    peerState := peer.GetPoHState()
    
    // 2. Создаём cross-reference: "в мой tick X, peer был в tick Y"
    crossRef := CrossRef{
        MyTick:      myState.Tick,
        MyHash:      myState.Hash,
        PeerID:      peerState.PeerID,
        PeerTick:    peerState.Tick,
        PeerHash:    peerState.Hash,
        WallClock:   myState.Timestamp,
        Signature:   sign(n.privKey, myState.Hash + peerState.Hash),
    }
    
    // 3. Сохраняем cross-reference локально
    n.crossRefs = append(n.crossRefs, crossRef)
    
    // 4. Peer делает то же самое (симметрично)
    peer.AddCrossRef(CrossRef{
        MyTick:    peerState.Tick,
        MyHash:    peerState.Hash,
        PeerID:    myState.PeerID,
        PeerTick:  myState.Tick,
        PeerHash:  myState.Hash,
        WallClock: peerState.Timestamp,
        Signature: peer.Sign(myState.Hash + peerState.Hash),
    })
}
```

### Построение causal order

```go
// При проверке "событие E1 произошло до E2?":
func (n *Node) HappenedBefore(e1, e2 *PoHEvent) bool {
    // Case 1: оба события на одном узле
    if e1.PeerID == e2.PeerID {
        return e1.Tick < e2.Tick
    }
    
    // Case 2: есть прямая cross-reference
    for _, ref := range n.crossRefs {
        if ref.PeerID == e1.PeerID && ref.MyTick >= e2.Tick {
            // e1 happened at ref.PeerTick, which maps to ref.MyTick
            // ref.MyTick >= e2.Tick, so e1 happened before e2
            return ref.PeerTick <= e1.Tick
        }
    }
    
    // Case 3: транзитивная связь через третий узел
    // A -> B -> C: если A happened before B и B happened before C, то A before C
    for _, intermediate := range n.knownPeers {
        if n.HappenedBefore(e1, lastEventOf(intermediate)) &&
           n.HappenedBefore(lastEventOf(intermediate), e2) {
            return true
        }
    }
    
    // Case 4: нет связи — события concurrent (неупорядочены)
    return false
}
```

### Верификация cross-references

```go
func (n *Node) VerifyCrossRef(ref CrossRef) bool {
    // 1. Проверяем подпись
    if !verify(ref.Signature, ref.MyHash + ref.PeerHash, ref.PeerID) {
        return false
    }
    
    // 2. Проверяем PoH-цепочку до ref.MyTick
    if !n.poh.VerifyUpTo(ref.MyTick, ref.MyHash) {
        return false
    }
    
    // 3. Проверяем PoH-цепочку peer'а до ref.PeerTick
    peerStream := n.getPeerStream(ref.PeerID)
    if !peerStream.VerifyUpTo(ref.PeerTick, ref.PeerHash) {
        return false
    }
    
    return true
}
```

### Практическое применение

Cross-references позволяют:
1. **Обнаруживать подделку времени**: если узел утверждает "я релеил в tick X",
   но cross-reference показывает что в этот момент он был офлайн — fraud
2. **Устанавливать порядок событий**: для reputation gossip важно знать
   "узел A релеил 1000 байт ДО того как узел B релеил 500 байт"
3. **Синхронизировать DHT-записи**: при конфликте версий побеждает та,
   у которой более ранний PoH-tick (по cross-references)

### Ограничения

- **Частичный порядок**: не все события можно упорядочить. Если два узла
  никогда не встречались и нет транзитивной связи — их события concurrent
- **Wall clock ненадёжен**: используем только как hint, не как доказательство
- **Cross-references накапливаются**: периодически prune старые (старше 30 дней)

Это даёт **частичный порядок** (partial ordering) событий между всеми узлами,
с которыми узел общался — даже без глобального времени.

## PoH в контексте консенсуса DHT

Когда узел публикует обновление reputation в DHT:

```go
dht.Put("reputation:" + peerID, &ReputationUpdate{
    Score:     newScore,
    PoHProof:  n.poh.GenerateProof(startTick, endTick),
    Signature: sign(n.privKey, newScore),
})
```

Другие узлы при получении:
1. Проверяют PoHProof (последовательность SHA256)
2. Проверяют подпись
3. Принимают обновление как valid

Невозможно подделать старый score как новый — PoH доказывает время.

## Модель хранения PoH

**Сырые тики НЕ хранятся.** 2.5M хэшей/сек × 32 байта = 80 MB/с — за день 7 TB.
Это неприемлемо для роутера с 128 MB RAM.

Вместо этого хранятся:

1. **Checkpoint'ы**: один хэш каждые 10 минут
   - 6 checkpoint'ов/час × 32 байта × 24 часа = **4.6 KB/день**

2. **Записи событий**: только когда произошло событие
   - ~1000 событий/день × ~100 байт/событие = **100 KB/день**
   - Событие = {type, data_hash, poh_tick_index, signature}

3. **Текущий seed + последний хэш** — для продолжения цепочки
   - **64 байта** (seed + last_hash)

**Итого: ~105 KB/день** — помещается на любом устройстве.

```
Структура хранения:
┌─────────────────────────────────────────┐
│ PoH Store                               │
│ ┌─────────────────────────────────────┐ │
│ │ Seed: 0xABCD... (32B)              │ │
│ │ LastTick: 57392000                 │ │
│ │ LastHash: 0x1234... (32B)         │ │
│ ├─────────────────────────────────────┤ │
│ │ Checkpoints (каждые 10 мин):       │ │
│ │  tick_0       → hash_0             │ │
│ │  tick_15000000 → hash_15000000      │ │
│ │  tick_30000000 → hash_30000000      │ │
│ │  ...                               │ │
│ ├─────────────────────────────────────┤ │
│ │ Events:                             │ │
│ │  tick_57392   → relay_0xFFA (1500B) │ │
│ │  tick_58103   → tx_0xBEEF  (100R)  │ │
│ │  tick_59421   → relay_0xCC2  (800B) │ │
│ └─────────────────────────────────────┘ │
└─────────────────────────────────────────┘
```

### Проверка старого события

Чтобы проверить "relay_0xFFA был в tick 57392":
1. Найти ближайший checkpoint ≤ 57392 (например, tick_0)
2. Пересчитать SHA256 от tick_0 до tick_57392 (57392 итераций, ~23 мс на CPU)
3. Сравнить полученный хэш с хэшем, подписанным в событии
4. Если совпадает — событие подлинное

### Очистка старых данных

Checkpoint'ы и события старше 30 дней агрегируются:
- События: суммируются в reputation score (bytes_relayed, tx_volume) и удаляются
- Checkpoint'ы: оставляется один на день, остальные удаляются
- PoH-цепочка всегда верифицируема от genesis (по цепочке checkpoint'ов)

## Вычислительные затраты

- Генерация PoH: ~1 ядро CPU (SHA256), 2.5M хэшей/сек — фоновый процесс
- Верификация: пересчёт от checkpoint до события (миллионы итераций → миллисекунды)
- Хранение: ~105 KB/день → ~3 MB/месяц → умещается даже на роутере с 8 MB flash

## Что Solana даёт для mesh (резюме)

| Без PoH | С PoH |
|---|---|
| Подделка timestamp'ов возможна | Timestamp криптографически доказуем |
| Нужен глобальный консенсус на время | Каждый узел — свой источник времени |
| Сложно доказать порядок relay'ев | PoR с PoH — неоспоримое доказательство |
| Reputation можно накрутить | Reputation привязана к реальному времени работы |
