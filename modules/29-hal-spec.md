# Hardware Abstraction Layer (HAL) 🟡 Required

> **Required.** Без HAL прошивка не адаптируется под разное железо. ESP32, роутеры, PC — у всех разное CPU/RAM/радио. Одна прошивка должна работать на всём.

## Принцип

Прошивка RMN не должна требовать ручной настройки под конкретную модель устройства.
При первом запуске она **автоопределяет** доступное железо и адаптирует свою
конфигурацию.

```
┌─────────────────────────────────────┐
│         RMN Application             │
├─────────────────────────────────────┤
│         RMN Core (протокол)         │
├─────────────────────────────────────┤
│         HAL (этот модуль)           │
├──────────┬──────────┬───────────────┤
│ WiFi     │ LoRa     │ Ethernet      │
│ Driver   │ Driver   │ Driver        │
├──────────┴──────────┴───────────────┤
│         Hardware                    │
│  (CPU, RAM, Flash, Radios)         │
└─────────────────────────────────────┘
```

## Автоопределение железа

### CPU/RAM/Flash

```go
type SystemInfo struct {
    OS        string  // "linux", "freebsd"
    Arch      string  // "mips", "mipsle", "arm", "arm64", "amd64"
    CPU       string  // "MediaTek MT7621", "Broadcom BCM2711"
    CPUCores  int
    RAM       int     // MB
    Flash     int     // MB (total, from /proc/mtd or df)
    Hostname  string
}

func DetectSystem() SystemInfo {
    info := SystemInfo{}
    
    // Linux
    if _, err := os.Stat("/proc/cpuinfo"); err == nil {
        info.OS = "linux"
        info.Arch = detectArch()      // uname -m
        info.CPUCores = runtime.NumCPU()
        info.RAM = detectRAM()        // /proc/meminfo: MemTotal
        info.Flash = detectFlash()    // df / (root partition size)
        info.Hostname, _ = os.Hostname()
    }
    
    return info
}

func detectArch() string {
    cmd := exec.Command("uname", "-m")
    out, _ := cmd.Output()
    arch := strings.TrimSpace(string(out))
    switch arch {
    case "mips":     return "mips"
    case "mipsel":   return "mipsle"
    case "armv7l":   return "arm"
    case "aarch64":  return "arm64"
    case "x86_64":   return "amd64"
    default:         return arch
    }
}

func detectRAM() int {
    data, _ := os.ReadFile("/proc/meminfo")
    for _, line := range strings.Split(string(data), "\n") {
        if strings.HasPrefix(line, "MemTotal:") {
            parts := strings.Fields(line)
            if len(parts) >= 2 {
                kb, _ := strconv.Atoi(parts[1])
                return kb / 1024  // KB → MB
            }
        }
    }
    return 128  // fallback
}

func detectFlash() int {
    var stat syscall.Statfs_t
    syscall.Statfs("/", &stat)
    return int(stat.Blocks * uint64(stat.Bsize) / 1024 / 1024)
}
```

### Радио

```go
type RadioInfo struct {
    Type      string  // "wifi_24ghz", "wifi_5ghz", "lora_868", "lora_915"
    Chip      string  // "mt7620", "sx1276", "sx1262", "sx1268"
    Interface string  // "wlan0", "/dev/ttyUSB0"
    MaxPower  int     // dBm
    SupportedSF []int // для LoRa
    SupportedBW []int // для LoRa (125, 250, 500 kHz)
}

func DetectRadios() []RadioInfo {
    var radios []RadioInfo
    
    // WiFi: сканируем /sys/class/net
    ifaces, _ := os.ReadDir("/sys/class/net")
    for _, iface := range ifaces {
        if isWiFi(iface.Name()) {
            radios = append(radios, detectWiFi(iface.Name()))
        }
    }
    
    // LoRa: сканируем /dev/ttyUSB*
    usbDevices, _ := filepath.Glob("/dev/ttyUSB*")
    for _, dev := range usbDevices {
        if isLoRa(dev) {
            radios = append(radios, detectLoRa(dev))
        }
    }
    
    // LoRa: также проверяем /dev/ttyACM*
    acmDevices, _ := filepath.Glob("/dev/ttyACM*")
    for _, dev := range acmDevices {
        if isLoRa(dev) {
            radios = append(radios, detectLoRa(dev))
        }
    }
    
    return radios
}

func detectLoRa(device string) RadioInfo {
    // Отправляем AT-команду на устройство
    // ESP32 отвечает: "LORA: chip=SX1276, freq=868, sf=7-12"
    port, _ := serial.OpenPort(serial.Config{Name: device, Baud: 115200})
    defer port.Close()
    
    port.Write([]byte("AT+LORA?\r\n"))
    buf := make([]byte, 256)
    n, _ := port.Read(buf)
    response := string(buf[:n])
    
    return parseLoRaResponse(response, device)
}
```

## Адаптация конфигурации

### Выбор профиля

```go
type DeviceProfile struct {
    Role      string    // "relay", "node", "bridge"
    Features  []string  // возможности
    MaxRelay  int       // KB/s
    MaxStorage int      // MB
    MinRAM    int       // минимальные требования для этого профиля
}

func AutoSelectProfile(sys SystemInfo, radios []RadioInfo) DeviceProfile {
    hasEthernet := sys.OS == "linux" && hasInterface("eth0")
    hasLoRa := false
    for _, r := range radios {
        if strings.HasPrefix(r.Type, "lora") { hasLoRa = true }
    }
    
    // Выбираем профиль
    if sys.RAM < 64 || sys.Flash < 16 {
        return DeviceProfile{
            Role:       "relay",
            Features:   []string{"ping", "chat", "confirm_n"},
            MaxRelay:   50,  // 50 KB/s
            MaxStorage: 0,
            MinRAM:     32,
        }
    }
    
    if hasEthernet && hasLoRa && sys.RAM >= 256 {
        return DeviceProfile{
            Role:       "bridge",
            Features:   []string{"ping", "chat", "confirm_n", "file_transfer", "dht_store", "gossip", "bridge"},
            MaxRelay:   200,
            MaxStorage: sys.Flash / 4,
            MinRAM:     128,
        }
    }
    
    return DeviceProfile{
        Role:       "node",
        Features:   []string{"ping", "chat", "confirm_n", "file_transfer", "dht_store", "gossip"},
        MaxRelay:   100,
        MaxStorage: sys.Flash / 4,
        MinRAM:     64,
    }
}
```

### Адаптация параметров

```go
func AdaptToHardware(profile DeviceProfile, sys SystemInfo) Config {
    cfg := DefaultConfig()
    
    // Ограничиваем relay-нагрузку
    cfg.MaxRelayKB = profile.MaxRelay
    
    // Контент-хранилище
    cfg.MaxStorageMB = profile.MaxStorage
    
    // Кэши
    if sys.RAM >= 256 {
        cfg.DHTCacheSize = 5000   // много RAM → больше кэша
    } else if sys.RAM >= 128 {
        cfg.DHTCacheSize = 2000
    } else {
        cfg.DHTCacheSize = 500    // минимум
    }
    
    // Cover traffic
    if sys.RAM >= 128 && sys.Flash >= 64 {
        cfg.CoverTrafficRate = 1   // 1 пакет/сек
    } else {
        cfg.CoverTrafficRate = 10  // 1 пакет/10 сек (экономим)
    }
    
    // Onion hops
    if sys.RAM < 64 {
        cfg.MinRelayHops = 1  // слабое железо — 1 hop
    }
    
    return cfg
}
```

## LoRa-транспорт (ESP32 ↔ Роутер)

### Serial-протокол

```
Формат пакета между роутером и ESP32:

┌──────────┬──────────┬──────────┬───────────────┐
│ Length   │ Type     │ Flags    │ Payload       │
│ 2 bytes  │ 1 byte   │ 1 byte   │ 0..65535      │
│ big-end  │          │          │               │
└──────────┴──────────┴──────────┴───────────────┘

Type:
  0x01 — TX (отправить LoRa-пакет)
  0x02 — RX (принят LoRa-пакет)
  0x03 — STATUS (RSSI, SNR, частота)
  0x04 — CONFIG (частота, SF, мощность)
  0x05 — AT (AT-команда)

Flags:
  0x01 — ACK требуется
  0x02 — Фрагментированный пакет
  0x04 — Последний фрагмент
```

### Go-драйвер

```go
type LoRaTransport struct {
    port   serial.Port
    txCh   chan []byte
    rxCh   chan []byte
    config LoRaConfig
}

type LoRaConfig struct {
    Frequency float64  // МГц (868.1)
    Bandwidth int      // кГц (125)
    SF        int      // 7-12
    Power     int      // dBm (14)
}

func (l *LoRaTransport) Start() error {
    // Открываем serial порт
    port, err := serial.Open("/dev/ttyUSB0", &serial.Config{
        BaudRate: 115200,
        DataBits: 8,
        StopBits: 1,
        Parity:   serial.NoParity,
    })
    if err != nil {
        return err
    }
    l.port = port
    
    // Конфигурируем ESP32
    l.configureESP32(l.config)
    
    // Запускаем чтение из serial
    go l.readLoop()
    
    return nil
}

func (l *LoRaTransport) Send(packet []byte) error {
    // Фрагментируем если нужно
    maxPayload := sfMaxPayload(l.config.SF)
    if len(packet) > maxPayload {
        return l.sendFragmented(packet, maxPayload)
    }
    
    return l.sendFrame(0x01, packet)
}

func (l *LoRaTransport) sendFrame(msgType byte, payload []byte) error {
    frame := make([]byte, 4+len(payload))
    binary.BigEndian.PutUint16(frame[0:2], uint16(len(payload)))
    frame[2] = msgType
    frame[3] = 0x01  // ACK
    copy(frame[4:], payload)
    
    _, err := l.port.Write(frame)
    return err
}
```

## Параметры

| Параметр | Значение | Описание |
|---|---|---|
| `HAL_DETECT_INTERVAL` | 60 сек | Периодичность сканирования новых устройств |
| `SERIAL_BAUDRATE` | 115200 | Скорость serial-порта для ESP32 |
| `SERIAL_TIMEOUT` | 500 мс | Таймаут чтения из serial |
| `LORA_AUTO_CONFIG` | true | Автоматически конфигурировать ESP32 при старте |
