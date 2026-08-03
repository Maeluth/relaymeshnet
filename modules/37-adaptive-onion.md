# Adaptive Onion Routing 🟡 Required

> **Required.** Не все узлы могут позволить себе 3-hop onion. ESP32 с 520 KB RAM, роутер с 64 MB, PC с 512 MB — разное железо требует разного уровня анонимности. Адаптивный onion выбирает число hop'ов в зависимости от возможностей узла и канала.

## Принцип

```
Вместо жёсткого MIN_RELAY_HOPS=1 для всех:

  Узел с 512 MB RAM, WiFi:     3 hop (полная анонимность)
  Узел с 128 MB RAM, WiFi:     2 hop (баланс)
  Узел с 64 MB RAM, LoRa SF7:  1 hop (минимальная анонимность)
  Узел с 64 MB RAM, LoRa SF12: Onion-Lite (1 hop, сжатый)

Адаптация происходит автоматически на основе:
  - RAM узла
  - Типа канала (WiFi/LoRa)
  - Текущей загрузки CPU
  - Размера сообщения
```

## Уровни onion

```go
type OnionLevel int

const (
    OnionFull    OnionLevel = 3  // 3 hop, полная анонимность
    OnionMedium  OnionLevel = 2  // 2 hop, баланс
    OnionLight   OnionLevel = 1  // 1 hop, минимальная
    OnionLite    OnionLevel = 0  // 1 hop, сжатый пакет (для LoRa SF12)
    OnionNone    OnionLevel = -1 // без onion (только для control-трафика)
)

func (n *Node) SelectOnionLevel(msg *Message, path []string) OnionLevel {
    // Control-трафик — без onion
    if msg.IsControl() {
        return OnionNone
    }
    
    // Высокочувствительные данные — всегда полный onion
    if msg.Type == MSG_TRANSFER {
        return OnionFull
    }
    
    // Выбираем по каналу и железу
    channel := n.bestChannel(path)
    hw := n.hardware
    
    // WiFi + много RAM → полный onion
    if channel == "wifi" && hw.RAM >= 256 {
        return OnionFull
    }
    
    // WiFi + средне → 2 hop
    if channel == "wifi" && hw.RAM >= 128 {
        return OnionMedium
    }
    
    // LoRa SF7 → 1 hop
    if channel == "lora_sf7" {
        return OnionLight
    }
    
    // LoRa SF12 → onion-lite
    if channel == "lora_sf12" {
        return OnionLite
    }
    
    return OnionLight
}
```

## Onion-Lite (сжатый пакет)

Для LoRa SF12 (51 байт/фрейм) полный onion-пакет не влезает.
Onion-Lite использует сжатый формат:

```go
// Стандартный onion: 385 байт для M=200
// Onion-Lite: 85 байт для M=20

type OnionLitePacket struct {
    Version    byte          // 1
    NextHop    [8]byte       // сжатый peerID (первые 8 байт)
    Ciphertext []byte        // зашифрованное сообщение
    MAC        [16]byte      // Poly1305 MAC
}

func (n *Node) WrapOnionLite(msg []byte, targetPeerID [16]byte) []byte {
    // Используем только 1 слой шифрования
    nonce := generateNonce()
    
    // Сжатый nextHop (8 байт вместо 32)
    shortHop := targetPeerID[:8]
    
    // Шифруем
    encrypted := box.Seal(nil, msg, &nonce, &targetPub, &myPriv)
    
    packet := OnionLitePacket{
        Version:    1,
        NextHop:    shortHop,
        Ciphertext: encrypted,
    }
    
    return packet.Bytes()
}

func (n *Node) UnwrapOnionLite(packet []byte) ([]byte, error) {
    pkt := parseOnionLite(packet)
    
    // Проверяем что nextHop — это мы (первые 8 байт нашего peerID)
    if !bytes.Equal(pkt.NextHop, n.peerID[:8]) {
        return nil, ErrNotForUs
    }
    
    // Расшифровываем
    msg, ok := box.Open(nil, pkt.Ciphertext, &nonce, &senderPub, &myPriv)
    if !ok {
        return nil, ErrDecryptionFailed
    }
    
    return msg, nil
}
```

### Размеры пакетов

| Уровень | Hops | M=20 ("Hi") | M=200 | M=1024 |
|---|---|---|---|---|
| OnionFull | 3 | 205 B | 385 B | 1209 B |
| OnionMedium | 2 | 145 B | 325 B | 1149 B |
| OnionLight | 1 | 85 B | 265 B | 1089 B |
| OnionLite | 1 (сжатый) | 45 B | — | — |

## Адаптация под загрузку CPU

```go
func (n *Node) AdjustOnionLevel() {
    ticker := time.NewTicker(1 * time.Minute)
    for range ticker.C {
        cpuUsage := n.getCPUUsage()
        
        // Если CPU загружен > 80% — снижаем уровень onion
        if cpuUsage > 0.8 {
            n.maxOnionLevel = OnionLight
            n.log.Warn("High CPU load, reducing onion level to 1 hop")
        } else if cpuUsage < 0.5 {
            n.maxOnionLevel = OnionFull
        }
    }
}
```

## Адаптация под размер сообщения

```go
func (n *Node) SelectOnionBySize(msgSize int, channel string) OnionLevel {
    // Маленькие сообщения: полный onion (оверхед приемлем)
    if msgSize <= 200 {
        return OnionFull
    }
    
    // Средние: зависит от канала
    if msgSize <= 2000 {
        if channel == "wifi" {
            return OnionFull
        }
        return OnionLight
    }
    
    // Большие (файлы): минимальный onion
    // (E2E шифрование уже защищает содержимое)
    return OnionLight
}
```

## Приоритеты при выборе relay-узлов

```go
func (n *Node) SelectRelayForOnion(onionLevel OnionLevel, candidates []*Node) []*Node {
    var selected []*Node
    needHops := int(onionLevel)
    
    switch onionLevel {
    case OnionFull, OnionMedium:
        // Выбираем узлы с хорошей репутацией и WiFi
        for _, c := range candidates {
            if c.HasWiFi() && c.Reputation > 10 {
                selected = append(selected, c)
                if len(selected) >= needHops {
                    break
                }
            }
        }
        
    case OnionLight, OnionLite:
        // Выбираем любые доступные узлы (даже LoRa-only)
        for _, c := range candidates {
            if c.IsOnline() {
                selected = append(selected, c)
                if len(selected) >= needHops {
                    break
                }
            }
        }
    }
    
    return selected
}
```

## Мониторинг уровня анонимности

```go
type AnonymityMetrics struct {
    Timestamp       uint64
    FullOnionCount  uint64  // сообщений с 3-hop onion
    MediumCount     uint64  // с 2-hop
    LightCount      uint64  // с 1-hop
    LiteCount       uint64  // onion-lite
    NoneCount       uint64  // без onion (control)
    AvgHops         float64 // среднее число hop'ов
}

func (n *Node) LogAnonymityLevel(level OnionLevel) {
    n.anonMetrics.mu.Lock()
    defer n.anonMetrics.mu.Unlock()
    
    switch level {
    case OnionFull:   n.anonMetrics.FullOnionCount++
    case OnionMedium: n.anonMetrics.MediumCount++
    case OnionLight:  n.anonMetrics.LightCount++
    case OnionLite:   n.anonMetrics.LiteCount++
    case OnionNone:   n.anonMetrics.NoneCount++
    }
    
    total := n.anonMetrics.FullOnionCount + n.anonMetrics.MediumCount + 
             n.anonMetrics.LightCount + n.anonMetrics.LiteCount
    if total > 0 {
        n.anonMetrics.AvgHops = float64(
            3*n.anonMetrics.FullOnionCount +
            2*n.anonMetrics.MediumCount +
            1*n.anonMetrics.LightCount +
            1*n.anonMetrics.LiteCount,
        ) / float64(total)
    }
}
```

## Параметры

| Параметр | Значение | Описание |
|---|---|---|
| `ONION_CPU_THRESHOLD` | 0.8 | При загрузке CPU > порога — снижать уровень |
| `ONION_LITE_MAX_MSG` | 40 байт | Максимальный размер для Onion-Lite |
| `ONION_FULL_MIN_RAM` | 256 MB | Минимум RAM для OnionFull |
| `ONION_MEDIUM_MIN_RAM` | 128 MB | Минимум RAM для OnionMedium |
| `ANONYMITY_LOG_INTERVAL` | 1 час | Интервал логирования статистики анонимности |
