# Monitoring & Diagnostics 🟡 Required

> **Required.** Без мониторинга администратор не видит состояние сети. Без диагностики нельзя понять почему сообщение не доставлено. Это не «приятно иметь» — это базовый инструмент для поддержки сети.

## Принцип

Три уровня мониторинга:

```
L1: Self-check — узел мониторит сам себя (CPU, RAM, relay-статистика)
L2: Neighbor check — соседи проверяют друг друга (пинги, SNR, packet loss)
L3: Network-wide — агрегированная статистика через gossip
```

## L1: Self-Check

### Системные метрики

```go
type SystemMetrics struct {
    Timestamp   uint64
    CPUPercent  float64  // загрузка CPU %
    RAMUsed     int      // использовано RAM (MB)
    RAMTotal    int      // всего RAM (MB)
    FlashUsed   int      // использовано Flash (MB)
    FlashTotal  int      // всего Flash (MB)
    Uptime      uint64   // секунд с последнего запуска
    Temperature float64  // °C (если есть сенсор)
}

func (n *Node) CollectSystemMetrics() SystemMetrics {
    return SystemMetrics{
        Timestamp:   uint64(time.Now().Unix()),
        CPUPercent:  getCPUUsage(),
        RAMUsed:     getRAMUsed(),
        RAMTotal:    n.hardware.RAM,
        FlashUsed:   getFlashUsed(),
        FlashTotal:  n.hardware.Flash,
        Uptime:      uint64(time.Since(n.startTime).Seconds()),
        Temperature: getTemperature(),
    }
}
```

### Relay-метрики

```go
type RelayMetrics struct {
    Timestamp       uint64
    BytesRelayedIn  uint64  // принято и переслано
    BytesRelayedOut uint64  // отправлено как relay
    PacketsDropped  uint64  // потеряно (timeout, no ACK)
    PacketsRelayed  uint64  // успешно переслано
    AvgLatency      float64 // средняя задержка relay (ms)
    QueueSize       int     // текущий размер relay-очереди
    ConfirmNCount   uint64  // количество confirm-N операций
}
```

### Экономические метрики

```go
type EconomyMetrics struct {
    Timestamp   uint64
    Balance     int64    // текущий баланс (×100)
    LockedOut   int64    // заблокировано в pending
    CreditLimit int64    // максимальный кредит
    Reputation  float64  // текущая репутация
    Emissions   int64    // всего получено emission
    Burned      int64    // всего сожжено (burn)
    TransfersIn int64    // получено переводов
    TransfersOut int64   // отправлено переводов
}
```

### Агрегация и хранение

```go
// Метрики хранятся в кольцевом буфере (последние 24 часа)
type MetricStore struct {
    mu      sync.RWMutex
    system  CircularBuffer[SystemMetrics]  // 1440 записей (1/min × 24h)
    relay   CircularBuffer[RelayMetrics]   // 1440 записей
    economy CircularBuffer[EconomyMetrics] // 1440 записей
}

// Каждую минуту сохраняем снапшот
func (n *Node) MetricsLoop() {
    ticker := time.NewTicker(1 * time.Minute)
    for range ticker.C {
        n.metrics.system.Push(n.CollectSystemMetrics())
        n.metrics.relay.Push(n.CollectRelayMetrics())
        n.metrics.economy.Push(n.CollectEconomyMetrics())
    }
}
```

## L2: Neighbor Check

### Health Check

```go
type NeighborHealth struct {
    PeerID       string
    LastSeen     uint64   // Unix timestamp
    AvgRSSI      float64  // средний RSSI
    AvgSNR       float64  // средний SNR
    PacketLoss   float64  // доля потерянных пакетов (0..1)
    Latency      float64  // средняя задержка (ms)
    IsSymmetric  bool     // линк двусторонний?
    Status       string   // "healthy", "degraded", "down"
    LastCheck    uint64   // когда была последняя проверка
}

func (n *Node) CheckNeighbor(peerID string) NeighborHealth {
    health := NeighborHealth{
        PeerID:    peerID,
        LastCheck: uint64(time.Now().Unix()),
        Status:    "down",
    }
    
    // Пингуем
    start := time.Now()
    ack, err := n.ping(peerID)
    if err == nil {
        health.LastSeen = uint64(time.Now().Unix())
        health.Latency = float64(time.Since(start).Milliseconds())
        health.Status = "healthy"
        
        // Обновляем EWMA метрик
        health.PacketLoss = n.updatePacketLoss(peerID, 0)   // 0 потерь
        health.AvgRSSI = n.updateEWMA(peerID, ack.RSSI, 0.3)
        health.AvgSNR = n.updateEWMA(peerID, ack.SNR, 0.3)
    } else {
        health.PacketLoss = n.updatePacketLoss(peerID, 1)   // потеря
        health.Status = "degraded"
        
        if health.PacketLoss > 0.5 {
            health.Status = "down"
        }
    }
    
    health.IsSymmetric = n.checkSymmetric(peerID)
    
    return health
}
```

### Алерты

```go
func (n *Node) CheckAlerts(health NeighborHealth) {
    // Критический алерт: сосед недоступен > 5 минут
    if health.Status == "down" {
        n.alert(CriticalAlert{
            Type:    "neighbor_down",
            PeerID:  health.PeerID,
            Message: fmt.Sprintf("Neighbor %s is down", health.PeerID),
        })
    }
    
    // Warning: высокий packet loss
    if health.PacketLoss > 0.3 {
        n.alert(WarningAlert{
            Type:    "high_packet_loss",
            PeerID:  health.PeerID,
            Message: fmt.Sprintf("Packet loss %.1f%% for %s", 
                health.PacketLoss*100, health.PeerID),
        })
    }
    
    // Info: асимметричный линк
    if !health.IsSymmetric {
        n.alert(InfoAlert{
            Type:    "asymmetric_link",
            PeerID:  health.PeerID,
            Message: fmt.Sprintf("Asymmetric link detected with %s", health.PeerID),
        })
    }
}
```

## L3: Network-Wide Monitoring

### Gossip-агрегация

```go
type NetworkStatus struct {
    Timestamp     uint64
    OnlineNodes   int
    TotalNodes    int
    AvgReputation float64
    AvgBalance    int64
    TotalRelayKB  uint64   // общий relay-трафик за 24ч
    AvgLatency    float64  // средняя задержка доставки
    SuccessRate   float64  // доля успешно доставленных сообщений
    Alerts        []AlertSummary
}

// Каждый узел вычисляет своё видение сети и gossip'ит
func (n *Node) GossipNetworkStatus() {
    ticker := time.NewTicker(5 * time.Minute)
    for range ticker.C {
        status := n.computeLocalView()
        
        // Отправляем 3 случайным соседям
        neighbors := n.randomNeighbors(3)
        for _, nb := range neighbors {
            n.sendGossip(nb, status)
        }
    }
}

// При получении gossip от соседа — мержим
func (n *Node) MergeNetworkStatus(peerStatus NetworkStatus) {
    n.mu.Lock()
    defer n.mu.Unlock()
    
    // EWMA-сглаживание глобальных метрик
    alpha := 0.3
    n.globalStatus.OnlineNodes = int(alpha*float64(peerStatus.OnlineNodes) + 
        (1-alpha)*float64(n.globalStatus.OnlineNodes))
    n.globalStatus.AvgLatency = alpha*peerStatus.AvgLatency + 
        (1-alpha)*n.globalStatus.AvgLatency
    n.globalStatus.SuccessRate = alpha*peerStatus.SuccessRate + 
        (1-alpha)*n.globalStatus.SuccessRate
    
    // Собираем алерты со всех узлов
    n.globalStatus.Alerts = mergeAlerts(n.globalStatus.Alerts, peerStatus.Alerts)
}
```

## Диагностика

### Трассировка сообщения

```go
type MessageTrace struct {
    MessageID  string
    Sender     string
    Receiver   string
    Status     string     // "sent", "in_transit", "delivered", "failed"
    Hops       []HopTrace
    CreatedAt  uint64
    UpdatedAt  uint64
}

type HopTrace struct {
    HopIndex   int
    RelayID    string
    ReceivedAt uint64
    ForwardedAt uint64
    Status     string   // "pending", "acked", "failed"
    FailReason string   // "timeout", "no_route", "offline"
}

func (n *Node) TraceMessage(msgID string) (*MessageTrace, error) {
    // Ищем в локальной истории
    trace := n.messageHistory.Find(msgID)
    if trace != nil {
        return trace, nil
    }
    
    // Запрашиваем у соседей через DHT
    traceRecord := n.dht.Get("trace:" + msgID)
    if traceRecord != nil {
        return parseTrace(traceRecord.Value), nil
    }
    
    return nil, ErrMessageNotFound
}
```

### WebUI: Панель администратора

```
┌─────────────────────────────────────────────────────────────┐
│  Админ-панель                                                │
│ ┌─────────────────────────────────────────────────────────┐ │
│ │ Система                                                  │ │
│ │ CPU: 23% | RAM: 45/128 MB | Flash: 32/64 MB            │ │
│ │ Uptime: 14д 6ч 23м | Temp: 47°C                         │ │
│ ├─────────────────────────────────────────────────────────┤ │
│ │ Relay                                                    │ │
│ │ За 24ч: 1.2 GB relayed | 99.7% success | 45ms avg      │ │
│ │ Очередь: 12 пакетов | Confirm-N: 342                     │ │
│ ├─────────────────────────────────────────────────────────┤ │
│ │ Экономика                                                │ │
│ │ Баланс: 1,234 RELAY | Репутация: 56.3                   │ │
│ │ Emission: 89 | Burned: 12 | Transfers: 5                │ │
│ ├─────────────────────────────────────────────────────────┤ │
│ │ Соседи (8)                                               │ │
│ │ 0xABCD ★ healthy, SNR 12dB, 0.1% loss                   │ │
│ │ 0x1234 ★ healthy, SNR 8dB, 0.3% loss                    │ │
│ │ 0x5678 ⚠ degraded, SNR 2dB, 15% loss                    │ │
│ │ 0x9ABC ✗ down, last seen 12м назад                       │ │
│ ├─────────────────────────────────────────────────────────┤ │
│ │ Трассировка                                              │ │
│ │ MsgID: [________________] [Трассировать]                 │ │
│ │ Sender → R1 (5ms) → R2 (8ms) → R3 (timeout) → FAIL      │ │
│ └─────────────────────────────────────────────────────────┘ │
└─────────────────────────────────────────────────────────────┘
```

### API для внешнего мониторинга

```go
// GET /api/admin/metrics — текущие метрики
// GET /api/admin/neighbors — состояние соседей
// GET /api/admin/alerts — последние алерты
// GET /api/admin/trace?msgid=XXX — трассировка сообщения
// POST /api/admin/ping?peer=XXX — пинг конкретного соседа
// GET /api/admin/network — агрегированное состояние сети
```

## Параметры

| Параметр | Значение | Описание |
|---|---|---|
| `METRICS_INTERVAL` | 1 минута | Интервал сбора метрик |
| `GOSSIP_INTERVAL` | 5 минут | Интервал gossip-агрегации |
| `NEIGHBOR_CHECK_INTERVAL` | 1 минута | Интервал проверки соседей |
| `NEIGHBOR_DOWN_THRESHOLD` | 5 минут | Через сколько считать соседа мёртвым |
| `PACKET_LOSS_WARN` | 0.1 | Порог packet loss для warning |
| `PACKET_LOSS_CRITICAL` | 0.5 | Порог packet loss для critical alert |
| `METRICS_RETENTION` | 24 часа | Сколько хранить историю метрик |
