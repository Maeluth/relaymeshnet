# LoRa Driver (ESP32 ↔ Router) 🔴 Core

> **Core.** Без спецификации LoRa-драйвера транспортный уровень не может работать с реальным железом. Это мост между Go-кодом на роутере и C-кодом на ESP32.

## Архитектура

```
┌──────────────────────────┐
│     RMN Node (Go)        │  Роутер OpenWRT
│  internal/transport/     │
│     lora_driver.go       │
├──────────────────────────┤
│     Serial (/dev/ttyUSB0)│  UART 115200 baud
├──────────────────────────┤
│   ESP32 Firmware (C)     │  ESP32 + SX1276
│     lora_rmn.ino         │
└──────────────────────────┘
```

## Serial Protocol v2

```
Фрейм:
┌──────────┬──────────┬──────────┬──────────┬───────────────┐
│ Magic    │ Length   │ Type     │ Flags    │ Payload       │
│ 2 bytes  │ 2 bytes  │ 1 byte   │ 1 byte   │ 0..65535      │
│ 0x4C52   │ big-end  │          │          │               │
│ ("LR")   │          │          │          │               │
└──────────┴──────────┴──────────┴──────────┴───────────────┘

CRC16 (2 bytes) в конце каждого фрейма.
```

### Типы сообщений (Type)

```go
const (
    TypeTX        = 0x01  // Отправить LoRa-пакет
    TypeRX        = 0x02  // Принят LoRa-пакет (от ESP32 к роутеру)
    TypeStatus    = 0x03  // Запрос/ответ статуса
    TypeConfig    = 0x04  // Конфигурация ESP32
    TypeError     = 0x05  // Ошибка
    TypeACK       = 0x06  // Подтверждение приёма фрейма
    TypeNACK      = 0x07  // Ошибка приёма (CRC mismatch)
    TypeReset     = 0x08  // Сброс ESP32
    TypeBootloader = 0x09 // Вход в режим прошивки
)
```

### Флаги (Flags)

```go
const (
    FlagACK       = 0x01  // Требуется подтверждение
    FlagFragmented = 0x02 // Пакет фрагментирован
    FlagLastFrag  = 0x04  // Последний фрагмент
    FlagEncrypted = 0x08  // Payload зашифрован (AES-256-CTR)
    FlagPriority  = 0x10  // Высокий приоритет
)
```

## Go-драйвер (на роутере)

```go
type LoRaDriver struct {
    port      io.ReadWriter
    config    LoRaConfig
    txQueue   chan *LoRaFrame
    rxQueue   chan *LoRaFrame
    statusCh  chan *LoRaStatus
    errCh     chan error
    
    // Статистика
    txFrames  uint64
    rxFrames  uint64
    crcErrors uint64
    timeouts  uint64
    
    // Для фрагментации
    pendingFrags map[uint16]*FragmentBuffer
    nextFragID   uint16
    
    mu sync.Mutex
}

type LoRaConfig struct {
    Frequency  float64 // МГц (868.1)
    Bandwidth  int     // кГц (125)
    SF         int     // 7-12
    CodingRate int     // 5 (4/5), 6 (4/6), 7 (4/7), 8 (4/8)
    Power      int     // dBm (14)
    Preamble   int     // символов (8)
    CRCon      bool    // CRC включён
    LBTEnabled bool    // Listen Before Talk
    LBTThreshold int   // RSSI порог для LBT (dBm)
}

type LoRaStatus struct {
    RSSI       int     // dBm
    SNR        float64 // dB
    Frequency  float64 // текущая частота
    SF         int     // текущий SF
    Temperature float64 // °C (ESP32)
    Uptime     uint64  // секунд
    FreeHeap   int     // байт свободно на ESP32
}

func NewLoRaDriver(device string, cfg LoRaConfig) (*LoRaDriver, error) {
    port, err := serial.Open(device, &serial.Config{
        BaudRate: 115200,
        DataBits: 8,
        StopBits: 1,
        Parity:   serial.NoParity,
        ReadTimeout: 500 * time.Millisecond,
    })
    if err != nil {
        return nil, fmt.Errorf("open serial: %w", err)
    }
    
    d := &LoRaDriver{
        port:         port,
        config:       cfg,
        txQueue:      make(chan *LoRaFrame, 100),
        rxQueue:      make(chan *LoRaFrame, 100),
        statusCh:     make(chan *LoRaStatus, 10),
        errCh:        make(chan error, 10),
        pendingFrags: make(map[uint16]*FragmentBuffer),
    }
    
    // Конфигурируем ESP32
    if err := d.configure(); err != nil {
        return nil, err
    }
    
    // Запускаем read loop
    go d.readLoop()
    
    return d, nil
}

func (d *LoRaDriver) configure() error {
    cfg := d.config
    
    // Отправляем конфигурацию на ESP32
    frame := NewConfigFrame(cfg)
    if _, err := d.sendAndWaitACK(frame, 3*time.Second); err != nil {
        return fmt.Errorf("configure ESP32: %w", err)
    }
    
    // Проверяем статус
    status, err := d.GetStatus()
    if err != nil {
        return fmt.Errorf("get status: %w", err)
    }
    
    if status.Frequency != cfg.Frequency {
        return fmt.Errorf("frequency mismatch: got %.1f, want %.1f", 
            status.Frequency, cfg.Frequency)
    }
    
    return nil
}

func (d *LoRaDriver) Send(payload []byte) error {
    maxPayload := sfMaxPayload(d.config.SF)
    
    if len(payload) > maxPayload {
        return d.sendFragmented(payload, maxPayload)
    }
    
    frame := NewTXFrame(payload, 0, FlagACK)
    _, err := d.sendAndWaitACK(frame, d.config.AirTime(len(payload))*100)
    return err
}

func (d *LoRaDriver) sendFragmented(payload []byte, maxPayload int) error {
    fragID := d.nextFragID
    d.nextFragID++
    
    totalFrags := (len(payload) + maxPayload - 1) / maxPayload
    
    for i := 0; i < totalFrags; i++ {
        start := i * maxPayload
        end := start + maxPayload
        if end > len(payload) {
            end = len(payload)
        }
        
        flags := FlagFragmented
        if i == totalFrags-1 {
            flags |= FlagLastFrag
        }
        
        header := []byte{
            byte(fragID >> 8), byte(fragID),
            byte(totalFrags >> 8), byte(totalFrags),
            byte(i >> 8), byte(i),
        }
        
        chunk := append(header, payload[start:end]...)
        frame := NewTXFrame(chunk, 0, byte(flags)|FlagACK)
        
        _, err := d.sendAndWaitACK(frame, 5*time.Second)
        if err != nil {
            return fmt.Errorf("fragment %d/%d: %w", i+1, totalFrags, err)
        }
    }
    
    return nil
}
```

## ESP32 Firmware (C/Arduino)

```cpp
// lora_rmn.ino — прошивка ESP32

#include <SPI.h>
#include <LoRa.h>

#define SERIAL_BAUD 115200
#define LORA_FREQ   868.1E6
#define LORA_SF     7
#define LORA_BW     125E3
#define LORA_CR     5
#define LORA_POWER  14

void setup() {
    Serial.begin(SERIAL_BAUD);
    
    if (!LoRa.begin(LORA_FREQ)) {
        Serial.println("ERR:LoRa init failed");
        while (1);
    }
    
    LoRa.setSpreadingFactor(LORA_SF);
    LoRa.setSignalBandwidth(LORA_BW);
    LoRa.setCodingRate4(LORA_CR);
    LoRa.setTxPower(LORA_POWER);
    
    // Прерывание на приём пакета
    LoRa.onReceive(onLoRaReceive);
    LoRa.receive();
}

void loop() {
    // Обработка команд от роутера
    if (Serial.available() >= 4) {
        processSerialFrame();
    }
    
    // Переодический статус (каждые 5 сек)
    static unsigned long lastStatus = 0;
    if (millis() - lastStatus > 5000) {
        sendStatus();
        lastStatus = millis();
    }
}

void processSerialFrame() {
    // Читаем magic bytes "LR"
    uint8_t magic[2];
    Serial.readBytes(magic, 2);
    if (magic[0] != 'L' || magic[1] != 'R') {
        sendNACK(0x01);  // bad magic
        return;
    }
    
    // Читаем длину
    uint16_t length = readUint16();
    
    // Читаем type и flags
    uint8_t type = Serial.read();
    uint8_t flags = Serial.read();
    
    // Читаем payload
    uint8_t payload[length];
    Serial.readBytes(payload, length);
    
    // Читаем CRC16
    uint16_t crc = readUint16();
    uint16_t calcCRC = crc16(payload, length);
    
    if (crc != calcCRC) {
        sendNACK(0x02);  // bad CRC
        return;
    }
    
    // Обрабатываем команду
    switch (type) {
        case 0x01:  // TX
            handleTX(payload, length, flags);
            break;
        case 0x03:  // Status
            sendStatus();
            break;
        case 0x04:  // Config
            handleConfig(payload, length);
            break;
        case 0x08:  // Reset
            ESP.restart();
            break;
    }
    
    // ACK если требуется
    if (flags & 0x01) {
        sendACK();
    }
}

void handleTX(uint8_t* payload, uint16_t length, uint8_t flags) {
    // LBT (Listen Before Talk) если включён
    if (config.lbtEnabled) {
        int rssi = LoRa.rssi();
        if (rssi > config.lbtThreshold) {
            sendError(0x10);  // channel busy
            return;
        }
    }
    
    // Отправляем LoRa-пакет
    LoRa.beginPacket();
    LoRa.write(payload, length);
    LoRa.endPacket(flags & 0x01);  // async = !ACK
    
    stats.txFrames++;
}

void onLoRaReceive(int packetSize) {
    uint8_t payload[256];
    int i = 0;
    while (LoRa.available() && i < 256) {
        payload[i++] = LoRa.read();
    }
    
    // Отправляем роутеру
    sendRXFrame(payload, i, LoRa.packetRssi(), LoRa.packetSnr());
    
    stats.rxFrames++;
}
```

## Обработка ошибок

### Переподключение при потере связи

```go
func (d *LoRaDriver) healthCheck() {
    ticker := time.NewTicker(10 * time.Second)
    for range ticker.C {
        _, err := d.GetStatus()
        if err != nil {
            d.errCh <- fmt.Errorf("ESP32 health check failed: %w", err)
            
            // Пробуем переподключиться
            d.reconnect()
        }
    }
}

func (d *LoRaDriver) reconnect() {
    for attempt := 0; attempt < 5; attempt++ {
        time.Sleep(time.Duration(attempt*2) * time.Second)
        
        if err := d.configure(); err == nil {
            log.Info("ESP32 reconnected")
            return
        }
    }
    
    log.Error("ESP32 reconnection failed after 5 attempts")
}
```

### CRC и целостность

```go
func crc16(data []byte) uint16 {
    var crc uint16 = 0xFFFF
    for _, b := range data {
        crc ^= uint16(b) << 8
        for i := 0; i < 8; i++ {
            if crc&0x8000 != 0 {
                crc = (crc << 1) ^ 0x1021
            } else {
                crc <<= 1
            }
        }
    }
    return crc
}
```

## Параметры

| Параметр | Значение | Описание |
|---|---|---|
| `SERIAL_BAUDRATE` | 115200 | Скорость UART |
| `FRAME_TIMEOUT` | 3 сек | Таймаут ожидания ACK |
| `MAX_RETRIES` | 3 | Максимум попыток отправки |
| `HEALTH_CHECK_INTERVAL` | 10 сек | Интервал проверки связи с ESP32 |
| `RECONNECT_ATTEMPTS` | 5 | Попыток переподключения |
| `LBT_THRESHOLD` | -90 dBm | Порог LBT (Listen Before Talk) |
