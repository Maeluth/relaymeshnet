# Gossip-Based DHT (альтернатива Kademlia) 🟡 Required

> **Required.** Kademlia предполагает стабильные соединения — в mesh с частыми разрывами она постоянно перестраивается, создавая лавину трафика. Gossip-DHT с bloom-фильтрами эффективнее для unreliable сетей.

## Проблема Kademlia в mesh

```
Kademlia:
  - Каждый узел хранит K ближайших по XOR-distance
  - При изменении топологии: FIND_NODE лавина
  - 500 узлов × 3 параллельных запроса = 1500 сообщений на изменение
  - На LoRa SF12 это ~45 минут только на DHT-трафик

Gossip-DHT:
  - Каждый узел хранит ВСЕ записи (или случайное подмножество)
  - При изменении: один gossip-пакет с bloom-фильтром
  - O(log n) распространение через SNR-based suppression
  - На LoRa SF12: ~2-3 минуты на полную синхронизацию
```

## Архитектура Gossip-DHT

### Хранение

```go
type GossipDHT struct {
    mu       sync.RWMutex
    store    map[string]*DHTRecord   // ключ → запись
    bloom    *BloomFilter             // для быстрой проверки наличия
    version  uint64                  // монотонно растущая версия
    lastSync map[string]uint64       // peerID → версия при последней синхронизации
}

type DHTRecord struct {
    Key       string
    Value     []byte
    TTL       uint64   // срок жизни (Unix timestamp)
    Version   uint64   // версия записи
    Publisher string   // кто опубликовал
    Signature []byte   // подпись публикатора
    CreatedAt uint64
    UpdatedAt uint64
}
```

### Bloom-фильтр для эффективной синхронизации

```go
type BloomFilter struct {
    bits      []uint64
    numHashes int
    size      int
}

func NewBloomFilter(expectedElements int, falsePositiveRate float64) *BloomFilter {
    // Оптимальный размер: m = -n*ln(p) / (ln(2))^2
    m := int(-float64(expectedElements) * math.Log(falsePositiveRate) / (math.Ln2 * math.Ln2))
    k := int(float64(m) / float64(expectedElements) * math.Ln2)  // оптимальное число хэшей
    
    return &BloomFilter{
        bits:      make([]uint64, (m+63)/64),
        numHashes: k,
        size:      m,
    }
}

func (bf *BloomFilter) Add(key string) {
    h1, h2 := hash(key)
    for i := 0; i < bf.numHashes; i++ {
        pos := (h1 + uint64(i)*h2) % uint64(bf.size)
        bf.bits[pos/64] |= 1 << (pos % 64)
    }
}

func (bf *BloomFilter) MightContain(key string) bool {
    h1, h2 := hash(key)
    for i := 0; i < bf.numHashes; i++ {
        pos := (h1 + uint64(i)*h2) % uint64(bf.size)
        if bf.bits[pos/64]&(1<<(pos%64)) == 0 {
            return false
        }
    }
    return true
}
```

### Gossip-синхронизация

```go
type GossipMessage struct {
    SenderID    string
    Version     uint64        // версия данных отправителя
    BloomFilter *BloomFilter  // bloom-фильтр всех ключей
    Timestamp   uint64
    Signature   []byte
}

// Отправляем gossip соседям
func (dht *GossipDHT) Gossip() {
    ticker := time.NewTicker(60 * time.Second)
    for range ticker.C {
        dht.mu.RLock()
        
        // Строим bloom-фильтр из всех ключей
        bf := NewBloomFilter(len(dht.store), 0.01)
        for key := range dht.store {
            bf.Add(key)
        }
        
        msg := &GossipMessage{
            SenderID:    dht.nodeID,
            Version:     dht.version,
            BloomFilter: bf,
            Timestamp:   uint64(time.Now().Unix()),
        }
        msg.Sign(dht.privKey)
        
        dht.mu.RUnlock()
        
        // Отправляем трём случайным соседям
        neighbors := dht.randomNeighbors(3)
        for _, nb := range neighbors {
            dht.sendGossip(nb, msg)
        }
    }
}

// При получении gossip от соседа
func (dht *GossipDHT) OnGossip(msg *GossipMessage) {
    if !msg.Verify(msg.SenderID) {
        return
    }
    
    dht.mu.Lock()
    defer dht.mu.Unlock()
    
    // Проверяем, новее ли версия соседа
    lastVer := dht.lastSync[msg.SenderID]
    if msg.Version <= lastVer {
        return  // уже синхронизированы
    }
    
    // Находим какие ключи есть у соседа, но нет у нас
    var missingKeys []string
    for key := range dht.store {
        if !msg.BloomFilter.MightContain(key) {
            // У соседа нет этого ключа — можем отправить ему
            // (опционально: отправляем только если наша версия новее)
        }
    }
    
    // Запрашиваем у соседа ключи, которых у нас нет
    // (отправляем свой bloom-фильтр в ответ)
    dht.requestMissingKeys(msg.SenderID, dht.buildBloomFilter())
    
    dht.lastSync[msg.SenderID] = msg.Version
}
```

### Запрос недостающих ключей

```go
type DeltaRequest struct {
    RequesterID string
    BloomFilter *BloomFilter  // какие ключи УЖЕ есть у запрашивающего
}

type DeltaResponse struct {
    Records []*DHTRecord  // записи, которых нет в bloom-фильтре
}

func (dht *GossipDHT) HandleDeltaRequest(req *DeltaRequest) *DeltaResponse {
    dht.mu.RLock()
    defer dht.mu.RUnlock()
    
    var missing []*DHTRecord
    for key, record := range dht.store {
        if !req.BloomFilter.MightContain(key) {
            missing = append(missing, record)
        }
    }
    
    return &DeltaResponse{Records: missing}
}
```

### Очистка устаревших записей

```go
func (dht *GossipDHT) Cleanup() {
    ticker := time.NewTicker(1 * time.Hour)
    for range ticker.C {
        dht.mu.Lock()
        
        now := uint64(time.Now().Unix())
        var expired []string
        
        for key, record := range dht.store {
            if record.TTL < now {
                expired = append(expired, key)
            }
        }
        
        for _, key := range expired {
            delete(dht.store, key)
        }
        
        dht.version++
        
        dht.mu.Unlock()
    }
}
```

## Сравнение с Kademlia

| | Kademlia | Gossip-DHT |
|---|---|---|
| **Поиск** | O(log n) FIND_NODE | Локальный (все записи хранятся) |
| **Обновление** | STORE на K ближайших узлах | Gossip-лавина с bloom-фильтром |
| **Трафик** | Высокий при нестабильной сети | Низкий (bloom-фильтры компактны) |
| **Память** | O(log n) записей на узел | O(всех записей) на узел |
| **Консистентность** | Сильная (Kademlia guarantees) | Eventual (gossip propagation) |
| **Partition tolerance** | Низкая (перестроение DHT) | Высокая (независимые копии) |

## Когда использовать

```
Kademlia:     > 1000 узлов, стабильная сеть, низкая latency
Gossip-DHT:   < 1000 узлов, нестабильная сеть, высокая latency (mesh!)

Для RMN: Gossip-DHT по умолчанию, Kademlia как fallback.
```

## Режим совместимости

```go
const (
    DHTModeGossip   = "gossip"    // для mesh (по умолчанию)
    DHTModeKademlia = "kademlia"  // для больших стабильных сетей
    DHTModeHybrid   = "hybrid"    // gossip внутри здания, kademlia между зданиями
)

func (dht *GossipDHT) SelectMode(networkSize int, avgUptime float64) string {
    if networkSize > 1000 && avgUptime > 0.95 {
        return DHTModeKademlia
    }
    return DHTModeGossip
}
```

## Параметры

| Параметр | Значение | Описание |
|---|---|---|
| `GOSSIP_INTERVAL` | 60 сек | Интервал gossip-синхронизации |
| `GOSSIP_NEIGHBORS` | 3 | Кому отправлять gossip |
| `BLOOM_FALSE_POSITIVE` | 0.01 | Допустимая ошибка bloom-фильтра |
| `CLEANUP_INTERVAL` | 1 час | Интервал очистки устаревших записей |
| `MAX_RECORDS_PER_NODE` | 10000 | Максимум хранимых записей |
| `SYNC_THRESHOLD` | 5 версий | При отставании > N — полная синхронизация |
