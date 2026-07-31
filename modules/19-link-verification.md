# Link Verification & Node Classes 🟡 Required

> **Required.** Без bidirectional проверки линков асимметричные соединения ломают маршрутизацию. Node classes нужны для выбора relay — без них battery-узлы используются как backbone.

## Проблема асимметричного линка

```
Узел A (мощный, окно, хорошая антенна) → Узел B (слабый, угол комнаты)
  A → B: SNR отличный, пакеты доходят  ✓
  B → A: SNR недостаточный, пакеты теряются  ✗

Без проверки:
  A думает: "B — сосед, релею через него"
  B думает: "A молчит, ушёл в офлайн"
  Оба имеют несогласованное представление о топологии
```

## Решение: Bidirectional Link Verification

### Протокол

```
1. DISCOVERY: A слышит HELLO от B
   → A НЕ добавляет B в таблицу маршрутизации

2. PROBE:  A → B: LINK_PROBE {TXPower, AntennaGain, RSSI_B_from_A}
   → B измеряет RSSI при приёме: "A слышен на −70 dBm"

3. ACK:    B → A: LINK_ACK {TXPower, AntennaGain, RSSI_A_measured_by_B}
   → Если A НЕ получает ACK за 5 секунд → линк АСИММЕТРИЧНЫЙ

4. CONFIRM: A → B: LINK_CONFIRM {RSSI_B_measured_by_A}
   → Теперь ОБА знают: линк двусторонний

5. Оба добавляют друг друга в routing table
```

### Что если линк асимметричный

```
A слышит B, но ACK не вернулся:
  → B помечается как RX_ONLY (A может принимать от B, но не отправлять)
  → B НЕ используется как следующий hop в onion-цепях
  → B НЕ используется для relay
  → A НЕ отправляет B пакеты, требующие ACK (PoR, подтверждения)

Но:
  → A всё ещё может ПРИНИМАТЬ сообщения от B (через relay-путь B→C→A)
  → A периодически перепроверяет линк (каждые 5 минут)
```

## Node Classes

### Категории оборудования

```go
type NodeClass struct {
    TXPower      int     // dBm
    AntennaGain  float64 // dBi
    MaxDutyCycle float64 // 0.01 (LoRa EU) .. 1.0 (WiFi/Ethernet)
    PowerSource  string  // "mains", "battery", "solar"
    Interfaces   []string // "wifi_2ghz", "wifi_5ghz", "lora_868", "ethernet"
    Position     int     // 0..10 (оценка выгодности: этаж, окно, угол)
}
```

### Предопределённые классы

| Класс | TX (dBm) | Power | Описание |
|---|---|---|---|
| **Default** | 14 | mains | Обычный роутер в комнате |
| **Booster** | 27 | mains | Усиленный, rooftop/backbone |
| **Mobile** | 14 | battery | Телефон/планшет через WiFi |
| **LowPower** | 10 | battery | Датчик, не для relay |
| **EtherBack** | — | mains | Ethernet backbone, без радио |

### Как классы влияют на маршрутизацию

```
Приоритет выбора relay-узла (веса):
  1. PowerSource = "mains"  → ×1.0
     PowerSource = "battery" → ×0.1 (почти не выбираем)
  2. Position ≥ 7           → ×1.5
     Position ≤ 3           → ×0.5
  3. TXPower ≥ 20           → ×1.3
  4. Имеет "lora" IF        → ×1.2 (межзданийский охват)
  5. Имеет "ethernet" IF    → ×2.0 (надёжный backbone)

Weight = base × power_factor × position_factor × tx_factor × IF_factors

Итог:
  Booster (27dBm, mains, rooftop, LoRa+Eth):
    W = 1.0 × 1.0 × 1.5 × 1.3 × 1.2 × 2.0 = 4.68
  Mobile (14dBm, battery, комната):
    W = 1.0 × 0.1 × 0.5 × 1.0 × 1.0 = 0.05
  Default (14dBm, mains, комната):
    W = 1.0 × 1.0 × 0.5 × 1.0 × 1.0 = 0.5

Booster выбирается в ~94 раза чаще чем Mobile, в ~9 раз чаще чем Default.
```

## Link Quality Table

Каждый узел поддерживает таблицу качества линков:

```go
type LinkQuality struct {
    PeerID         string
    SNR            float64  // последний измеренный SNR в обе стороны
    PacketLoss     float64  // доля потерянных ACK (0..1)
    Latency        float64  // средняя задержка ACK, мс
    LastVerified   uint64   // unix timestamp последней проверки
    IsSymmetric    bool     // линк двусторонний?
    Class          NodeClass
}
```

### Периодическая перепроверка

```
Каждые 5 минут для каждого соседа:
  1. Отправить LINK_PROBE
  2. Если ACK не вернулся за 2 секунды:
     PacketLoss += 0.1
     Если PacketLoss > 0.5: IsSymmetric = false
  3. Если ACK вернулся:
     PacketLoss *= 0.9 (экспоненциальное сглаживание)
     SNR = (SNR_old × 0.7 + SNR_new × 0.3) (EWMA)
     IsSymmetric = true
```

## Влияние на onion routing

```
При построении цепи:
  1. Запросить DHT: топ-100 узлов по reputation
  2. Отфильтровать: IsSymmetric == true
  3. Отфильтровать: PacketLoss < 0.3
  4. Отсортировать по Weight (из NodeClass)
  5. Выбрать случайные H узлов с weighted probability
  6. Построить onion-пакет

Перед отправкой через цепь:
  Проверить IsSymmetric для каждого hop'а
  Если hop стал асимметричным → заменить на fallback hop
```

## Параметры

| Параметр | Значение | Описание |
|---|---|---|
| `PROBE_TIMEOUT` | 5 сек | Таймаут ожидания LINK_ACK |
| `PROBE_INTERVAL` | 300 сек | Периодичность перепроверки линков |
| `PACKET_LOSS_THRESHOLD` | 0.5 | При превышении — линк считается асимметричным |
| `SYMMETRIC_REQUIRED` | true | Использовать только симметричные линки |
