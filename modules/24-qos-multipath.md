# Transport QoS & Multi-Path 🟡 Required

> **Required.** Без приоритезации трафика и multi-path сеть неэффективна: файлы забивают чат, LoRa используется когда WiFi быстрее. Это не «оптимизация» — без этого сеть деградирует под нагрузкой.

## Два режима доставки (TCP-like vs UDP-like)

В классическом интернете TCP гарантирует доставку ценой задержки (ACK на каждый сегмент),
UDP шлёт без проверки — быстрее, но негарантированно. В mesh эта дихотомия ещё важнее
из-за нестабильных каналов.

```
Reliable Mode (TCP-like, через onion):
  - Каждый hop подтверждает получение (hop-by-hop ACK)
  - End-to-end подтверждение через обратную цепь
  - Retransmission при потере
  - Для: переводы RELAY, файлы, критичные сообщения

Best-Effort Mode (UDP-like, onion-lite):
  - Без ACK, fire-and-forget
  - Быстрее в 2-3× (нет ожидания подтверждений)
  - Потерянный пакет = потерянное сообщение
  - Для: текстовый чат, статус-апдейты, streaming
```

### Сравнение задержки

```
Текстовое сообщение 200 байт, WiFi, 3 hop'а:

Reliable (TCP-like):
  S→R1: 5ms + ACK R1→S: 5ms = 10ms
  R1→R2: 5ms + ACK R2→R1: 5ms = 10ms
  R2→D: 5ms + ACK D→R2: 5ms = 10ms
  Итого: 30ms + обработка

Best-Effort (UDP-like):
  S→R1: 5ms
  R1→R2: 5ms
  R2→D: 5ms
  Итого: 15ms + обработка

Разница: ×2 быстрее
```

## Multi-Path: разделение трафика по каналам

WiFi и LoRa — не «или-или», а «и». Данные разбиваются и идут параллельно:

```
Файл 50 KB → chunk1, chunk2, chunk3, chunk4

  chunk1 → WiFi → R1 → D      (5ms)
  chunk2 → WiFi → R2 → D      (5ms)
  chunk3 → LoRa SF7 → D       (500ms, напрямую если рядом)
  chunk4 → LoRa SF12 → R3 → D (10 сек, дальний relay)

Получатель собирает: chunk1 (5ms), chunk2 (5ms), chunk3 (500ms), chunk4 (10s)
  → Файл готов через 10 сек (время самого медленного чанка)
  → Без multi-path: 4 × 10 сек = 40 сек (все через LoRa SF12)
```

### Алгоритм выбора канала

```go
func SelectChannel(msgSize int, hops int, availableChannels []Channel) []Channel {
    wifiPaths := filter(availableChannels, "wifi")
    loraPaths := filter(availableChannels, "lora")

    if msgSize <= 500 && len(loraPaths) > 0 {
        // Текст: пробуем WiFi, fallback LoRa
        if len(wifiPaths) > 0 {
            return wifiPaths[:1]
        }
        return loraPaths[:1]
    }

    if msgSize > 500 && msgSize <= 50000 {
        // Средний файл: striping WiFi + LoRa
        chunks := msgSize / 256 // чанки по 256 байт
        wifiChunks := chunks * 3 / 4  // 75% через WiFi
        loraChunks := chunks / 4      // 25% через LoRa

        selected := wifiPaths[:min(len(wifiPaths), wifiChunks)]
        selected = append(selected, loraPaths[:min(len(loraPaths), loraChunks)]...)
        return selected
    }

    // Большой файл: WiFi-only или очередь до появления WiFi
    if len(wifiPaths) > 0 {
        return wifiPaths
    }
    return nil // rejected — слишком большой для LoRa
}
```

## LoRa бюджет и приоритезация

LoRa SF12 = 293 бит/с × 86400 сек × 1% duty cycle ≈ 31 KB/день на узел.
Этот бюджет нужно распределять:

```
Бюджет LoRa-узла на день: 30 KB

Приоритеты расхода:
  1. Control (пинги, ACK, HELLO, DHT lookup) — до 10 KB
  2. Текст ≤ 500 байт (чат) — до 15 KB (~75 сообщений)
  3. Чанки файлов > 500 байт (multi-path) — до 5 KB
  4. Резерв — что осталось

Исчерпан бюджет → узел отклоняет LoRa-трафик данного приоритета
  → Отправитель получает "LoRa budget exhausted, retry WiFi or wait"
```

## Адаптивный выбор: WiFi через N hop'ов vs LoRa через M hop'ов

Все пользовательские сообщения идут через минимум 1 relay (MIN_RELAY_HOPS).
Но количество hop'ов и тип канала можно выбирать для оптимизации:

```
Узел A → Узел D (расстояние 50 м, но WiFi зашумлён):

WiFi путь: A→R1→R2→D (2 relay, 3 hop'а всего)
  Каждый hop: 5ms + обработка 5ms = 10ms
  Итого: 3 × 10ms = 30ms

LoRa SF7 путь: A→R3→D (1 relay, 2 hop'а всего)
  Air time + relay = 50ms + 50ms = 100ms

  → WiFi БЫСТРЕЕ даже с дополнительным relay!

LoRa SF7 путь: A→R3→D (1 relay)
  vs
WiFi путь: A→R1→R2→R3→R4→R5→D (5 relay, 6 hop'ов)
  WiFi: 6 × 10ms = 60ms
  LoRa: 2 × 50ms = 100ms

  → WiFi ВСЁ ЕЩЁ быстрее! LoRa проигрывает даже на 2 hop'ах.
```

### Правило выбора (с учётом MIN_RELAY_HOPS)

```
1. Все пользовательские сообщения: минимум MIN_RELAY_HOPS (1-3)
2. Служебный трафик (HELLO, ping, ACK, DHT): может идти напрямую
3. Выбор канала:
   WiFi с N hop'ами vs LoRa с M hop'ами (где N,M ≥ MIN_RELAY_HOPS)
   → Выбираем путь с минимальной estimated_latency

```go
func EstimatedLatency(path []string, channelType string) time.Duration {
    if channelType == "wifi" {
        return time.Duration(len(path)-1) * 10 * time.Millisecond
    }
    // LoRa: считаем air time + duty cycle
    sf, airTime := bestSF(path)
    return airTime * time.Duration(len(path)-1) / DutyCycleRate
}

// Выбираем путь с минимальной задержкой (с учётом MIN_RELAY_HOPS)
func BestPath(msg *Message, wifiPath, loraPath []string, minHops int) ([]string, string) {
    // Проверяем что пути удовлетворяют минимальному числу relay
    wifiRelays := len(wifiPath) - 2
    loraRelays := len(loraPath) - 2
    
    wifiValid := wifiRelays >= minHops
    loraValid := loraRelays >= minHops
    
    if !wifiValid && !loraValid {
        return nil, "" // нет подходящего пути
    }
    if !wifiValid {
        return loraPath, "lora"
    }
    if !loraValid {
        return wifiPath, "wifi"
    }
    
    wifiLat := EstimatedLatency(wifiPath, "wifi")
    loraLat := EstimatedLatency(loraPath, "lora")
    if wifiLat < loraLat {
        return wifiPath, "wifi"
    }
    return loraPath, "lora"
}
```

## Сжатие перед отправкой

```
Текстовое сообщение 200 байт:
  Без сжатия: onion = 200 + 185 = 385 байт
  С zlib/deflate: текст → 80 байт, onion = 80 + 185 = 265 байт
  Экономия: 31%

Файл 50 KB:
  Без сжатия: onion overhead 185 байт (ничтожно)
  Сжатие: ~10-30% экономии в зависимости от типа файла
  Но: сжатие ломает padding (все пакеты одного размера)
  → Сжатие только для best-effort режима (без cover traffic)
```

## Параметры

| Параметр | Значение | Описание |
|---|---|---|
| `WIFI_HOP_LATENCY` | 10 мс | Средняя задержка на WiFi-hop (передача + обработка) |
| `LORA_BUDGET_DAILY` | 30 KB | Дневной LoRa-бюджет узла |
| `LORA_CONTROL_BUDGET` | 10 KB | Из бюджета на control-трафик |
| `LORA_TEXT_BUDGET` | 15 KB | Из бюджета на текст |
| `LORA_FILE_BUDGET` | 5 KB | Из бюджета на чанки файлов |
| `COMPRESSION_THRESHOLD` | 500 байт | Сжимать сообщения больше этого размера |
| `STRIPING_CHUNK_SIZE` | 256 байт | Размер чанка для multi-path striping |
| `RELIABLE_MODE_TYPES` | transfer, file, firmware | Типы сообщений, требующие TCP-like доставки |
| `BEST_EFFORT_TYPES` | chat, status, ping | Типы сообщений, допускающие UDP-like доставку |
