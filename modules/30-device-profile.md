# Device Profile & Auto-Configuration 🟡 Required

> **Required.** Без автоопределения профиля пользователь должен вручную настраивать каждый узел. Для 500+ узлов это невозможно. Прошивка должна сама понять что она может и как работать.

## Принцип

При первом запуске прошивка:
1. Определяет железо (HAL, модуль 29)
2. Определяет роль (relay / node / bridge)
3. Определяет доступные фичи (capabilities)
4. Генерирует identity (ключи)
5. Сохраняет конфигурацию
6. Запускает bootstrap (модуль 12)

Всё автоматически. Пользователь только включает питание.

## Процесс первого запуска

```
┌─────────────────────────────────────────────────────┐
│                   ПЕРВЫЙ ЗАПУСК                       │
├─────────────────────────────────────────────────────┤
│  1. DetectHardware()  →  CPU, RAM, Flash, Radios    │
│  2. DetectRole()      →  relay / node / bridge      │
│  3. DetectFeatures()  →  список capabilities        │
│  4. GenerateIdentity()→  Ed25519 + Curve25519 keys  │
│  5. SaveConfig()      →  /etc/rmn/config.json       │
│  6. Bootstrap()       →  найти соседей              │
│  7. Run()             →  основная работа            │
└─────────────────────────────────────────────────────┘
```

## Определение роли

```go
type DeviceRole string

const (
    RoleRelay  DeviceRole = "relay"  // только relay, без UI
    RoleNode   DeviceRole = "node"   // полноценный узел
    RoleBridge DeviceRole = "bridge" // мост в интернет
)

func DetectRole(hw SystemInfo, radios []RadioInfo) DeviceRole {
    hasEthernet := detectEthernet()
    hasLoRa := hasLoRaRadio(radios)
    hasWiFi := hasWiFiRadio(radios)
    
    // Bridge: есть Ethernet + LoRa + достаточно RAM
    if hasEthernet && hasLoRa && hw.RAM >= 256 {
        return RoleBridge
    }
    
    // Relay: мало RAM или нет WiFi (только LoRa)
    if hw.RAM < 64 || (!hasWiFi && hasLoRa) {
        return RoleRelay
    }
    
    // Node: стандартный узел
    return RoleNode
}
```

## Capabilities по роли

```go
var RoleCapabilities = map[DeviceRole][]string{
    RoleRelay: {
        "ping", "chat", "confirm_n",  // базовые
    },
    RoleNode: {
        "ping", "chat", "confirm_n",
        "file_transfer", "dht_store", "gossip", "transfer",
        "x3dh",  // E2E encryption
    },
    RoleBridge: {
        "ping", "chat", "confirm_n",
        "file_transfer", "dht_store", "gossip", "transfer",
        "x3dh",
        "bridge", "service_host",  // дополнительные
    },
}

func DetectFeatures(role DeviceRole, hw SystemInfo) []string {
    caps := RoleCapabilities[role]
    
    // Добавляем опциональные фичи если хватает железа
    if hw.RAM >= 256 && hw.Flash >= 128 {
        caps = append(caps, "storage", "web_services")
    }
    
    if hw.RAM >= 512 {
        caps = append(caps, "content_storage", "firmware_cache")
    }
    
    return caps
}
```

## Генерация identity

```go
type DeviceIdentity struct {
    PeerID          [16]byte  // SHA256(Ed25519_pub)[:16]
    Ed25519Pub      [32]byte
    Ed25519Priv     [32]byte  // зашифрован (если есть пароль)
    Curve25519Pub   [32]byte
    Curve25519Priv  [32]byte
    CreatedAt       uint64    // Unix timestamp
    PoHSeed         [32]byte
}

func GenerateIdentity() (*DeviceIdentity, error) {
    id := &DeviceIdentity{
        CreatedAt: uint64(time.Now().Unix()),
    }
    
    // Ed25519 — для подписей, самосертификации
    edPub, edPriv, err := ed25519.GenerateKey(rand.Reader)
    if err != nil { return nil, err }
    copy(id.Ed25519Pub[:], edPub)
    copy(id.Ed25519Priv[:], edPriv)
    
    // Curve25519 — для E2E (X3DH)
    curvePub, curvePriv, err := curve25519.GenerateKey(rand.Reader)
    if err != nil { return nil, err }
    copy(id.Curve25519Pub[:], curvePub)
    copy(id.Curve25519Priv[:], curvePriv)
    
    // PeerID
    h := sha256.Sum256(edPub)
    copy(id.PeerID[:], h[:16])
    
    // PoH seed
    pohSeed := sha256.Sum256(append(edPub, id.CreatedAt.Bytes()...))
    copy(id.PoHSeed[:], pohSeed[:])
    
    return id, nil
}
```

## Сохранение конфигурации

```go
type RuntimeConfig struct {
    Version   int              // версия конфига (для миграции)
    Identity  DeviceIdentity
    Profile   DeviceProfile
    Radio     []RadioConfig
    Network   NetworkConfig
    Economy   EconomyConfig
    QoS       QoSConfig
}

type DeviceProfile struct {
    Role        string   // "relay", "node", "bridge"
    Features    []string // capabilities
    MaxRelayKB  int      // максимальная relay-нагрузка
    MaxStorageMB int     // максимальное хранилище
}

type RadioConfig struct {
    Type      string  // "lora_868", "wifi_24ghz"
    Enabled   bool
    Frequency float64
    Bandwidth int
    SF        int     // для LoRa
    Power     int     // dBm
}

type NetworkConfig struct {
    MeshSSID    string  // "RMN-XXXX"
    MeshIP      string  // 10.42.0.0/16
    MinRelayHops int
    MaxRelayHops int
}

type EconomyConfig struct {
    EmissionRate    float64
    BurnRate        float64
    CreditLimitBase float64
    DemurrageRate   float64
}

type QoSConfig struct {
    CoverTrafficRate  int   // пакетов/сек
    MaxLoRaBudgetKB   int   // KB/день
    WiFiOverLoRa      bool  // предпочитать WiFi
}
```

## Ручное переопределение

```go
// Через WebUI или config.json можно переопределить авто-параметры
func LoadConfig() RuntimeConfig {
    cfg := AutoDetectConfig()  // автоопределение
    
    // Проверяем есть ли config.json
    if data, err := os.ReadFile("/etc/rmn/config.json"); err == nil {
        var manual RuntimeConfig
        json.Unmarshal(data, &manual)
        cfg.MergeManual(manual)  // сливаем ручные настройки поверх авто
    }
    
    return cfg
}

func (c *RuntimeConfig) MergeManual(manual RuntimeConfig) {
    if manual.Profile.Role != "" {
        c.Profile.Role = manual.Profile.Role  // переопределяем роль
    }
    if len(manual.Profile.Features) > 0 {
        c.Profile.Features = manual.Profile.Features
    }
    // ... и т.д. для всех полей
}
```

## Параметры

| Параметр | Значение | Описание |
|---|---|---|
| `CONFIG_PATH` | /etc/rmn/config.json | Путь к файлу конфигурации |
| `KEY_PATH` | /etc/rmn/keys/ | Путь к приватным ключам |
| `AUTO_CONFIG_ENABLED` | true | Автоопределение при первом запуске |
| `MIN_RAM_NODE` | 64 MB | Минимум RAM для роли "node" |
| `MIN_RAM_BRIDGE` | 256 MB | Минимум RAM для роли "bridge" |
| `MIN_FLASH_NODE` | 16 MB | Минимум Flash для роли "node" |
