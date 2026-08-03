# Inter-Mesh Federation 🟡 Required

> **Required.** Одна mesh-сеть ограничена радиусом LoRa (10 км). Без федерации RMN не может выйти за пределы одного кампуса. Bridge-узлы соединяют разные mesh-сети через интернет в единую федерацию.

## Принцип

```
Mesh A (кампус А, 500 узлов)        Mesh B (кампус Б, 500 узлов)
     │                                      │
     ├── Bridge_A1 ──┐                      ├── Bridge_B1 ──┐
     │               │                      │               │
     │         Tor/VPN туннель              │         Tor/VPN туннель
     │               │                      │               │
     └───────────────┴── единая федерация ──┴───────────────┘

Для узла внутри Mesh A:
  → Узел из Mesh B выглядит как обычный peer (доступен через bridge)
  → Новый peerID виден в DHT (gossip между bridge'ами)
  → Сообщение идёт: WiFi/LoRa → Bridge_A → интернет → Bridge_B → WiFi/LoRa
```

## Интернет как транспорт

Bridge-узлы соединяются через TCP-туннель. Весь интернет-маршрут — **один логический hop** для RMN:

```go
type InterMeshTunnel struct {
    LocalBridge  string       // peerID нашего bridge
    RemoteBridge string       // peerID удалённого bridge
    RemoteAddr   string       // "tor-address.onion:9001" или "vpn-ip:9001"
    Transport    string       // "tor", "wireguard", "tls"
    Established  time.Time
    BytesSent    uint64
    BytesRecv    uint64
}

func (t *InterMeshTunnel) Connect() error {
    switch t.Transport {
    case "tor":
        // Tor SOCKS5 → .onion адрес
        dialer, _ := proxy.SOCKS5("tcp", "127.0.0.1:9050", nil, proxy.Direct)
        conn, err := dialer.Dial("tcp", t.RemoteAddr)
        
    case "wireguard":
        // WireGuard VPN-туннель
        conn, err := net.Dial("tcp", t.RemoteAddr)
        
    case "tls":
        // Прямой TLS (если анонимность не критична)
        conn, err := tls.Dial("tcp", t.RemoteAddr, &tls.Config{...})
    }
    
    // Handshake RMN поверх TCP
    t.doRMNHandshake(conn)
    
    return nil
}
```

## Маршрутизация между mesh'ами

```
Узел в Mesh A хочет отправить сообщение узлу в Mesh B:

1. DHT lookup: peerID_боба → не найден локально
2. Bridge_A: "есть кто в федерации?"
3. Bridge_B: "да, peerID_боба у нас"
4. Bridge_A ↔ Bridge_B: устанавливают/проверяют туннель
5. Сообщение идёт:
   [Узел A] → WiFi → [Relay] → WiFi → [Bridge_A] → Tor → [Bridge_B] → LoRa → [Узел Б]

Для узла A: всё выглядит как обычное сообщение (onion routing скрывает путь)
```

## DHT в федерации

Bridge'и обмениваются DHT-записями через gossip поверх TCP-туннелей:

```go
// Bridge периодически синхронизирует DHT с другими bridge'ами
func (b *Bridge) SyncFederationDHT() {
    ticker := time.NewTicker(5 * time.Minute)
    for range ticker.C {
        for _, tunnel := range b.tunnels {
            // Отправляем bloom-фильтр своих DHT-ключей
            bf := b.buildBloomFilter()
            b.sendDHTGossip(tunnel, bf)
        }
    }
}
```

## Discovery: как mesh'и находят друг друга

### Режим A: Ручное соединение

```bash
# На Bridge_A:
rmn-bridge peer add --peer-id 0xBOB_BRIDGE --addr tor://bob-bridge.onion:9001
```

### Режим B: Глобальный DHT-сервер

```
Интернет-DHT (bootstrap-сервер, общедоступный):
  "mesh:ru-campus-1" → {bridge_peerID, tor_addr, nodes: 500, region: "RU"}
  "mesh:us-dorm-3"   → {bridge_peerID, tor_addr, nodes: 300, region: "US"}

Bridge при старте:
  1. Подключается к bootstrap-серверу (известный .onion адрес)
  2. Регистрирует свою mesh
  3. Получает список других mesh'ей
  4. Пользователь (админ bridge) решает к каким подключиться
```

### Режим C: Gossip-распространение (децентрализованный)

```
Bridge_A знает Bridge_B, Bridge_B знает Bridge_C
→ Bridge_B gossip'ит: "я знаю Bridge_C, вот его адрес"
→ Bridge_A узнаёт о Bridge_C через Bridge_B
→ Цепочка доверия: A доверяет B, B доверяет C → A может подключиться к C
```

## Память на роутере

Роутер НЕ хранит все peerID'ы федерации. Только:

```go
type RouterCache struct {
    LocalPeers    map[string]*PeerInfo  // ~50 записей (WiFi/LoRa соседи)
    DHTCache      map[string]*DHTRecord // ~2000 записей (своя XOR-зона)
    RouteCache    map[string][]string   // ~100 записей (кэш маршрутов)
    FederationHint string              // "если не нашёл → спроси bridge X"
}
```

При запросе неизвестного peerID: DHT lookup → если нет локально → bridge → федерация.

## Трафик и стоимость

```
Пакет из Mesh A в Mesh B:
  1. WiFi/LoRa внутри Mesh A:     N hop'ов (бесплатно, confirm-N)
  2. Bridge_A → интернет:         трафик через ISP (платит bridge-оператор)
  3. TCP-туннель между bridge'ами: overhead ~40 байт (TLS) или ~60 байт (Tor)
  4. Bridge_B → WiFi/LoRa в Mesh B: N hop'ов (бесплатно)

Bridge-оператор окупает интернет-трафик через:
  - Комиссию с внешних сообщений (BRIDGE_REWARD, модуль 07)
  - Продажу RELAY внешним пользователям
  - Абонентскую плату с узлов своей mesh
```

## Топология федерации

```
          ┌──────────┐         ┌──────────┐
          │  Mesh A  │         │  Mesh B  │
          │ 500 узлов│◄────────►│ 300 узлов│
          └────┬─────┘  Tor    └────┬─────┘
               │                    │
               │    ┌──────────┐    │
               └────┤  Mesh C  ├────┘
                    │  50 узлов│
                    └──────────┘

Полносвязная (каждый bridge с каждым):     N×(N-1)/2 туннелей
Цепочка (A-B, B-C):                        N-1 туннелей
Звезда (все через хаб-бридж):              N туннелей

Рекомендация: полносвязная для N≤5, звезда для N>5.
```

## Параметры

| Параметр | Значение | Описание |
|---|---|---|
| `FEDERATION_TUNNEL_PROTO` | tor | tor / wireguard / tls |
| `FEDERATION_GOSSIP_INTERVAL` | 5 минут | Интервал DHT-синхронизации |
| `FEDERATION_MAX_TUNNELS` | 10 | Максимум туннелей на bridge |
| `FEDERATION_RECONNECT_INTERVAL` | 60 сек | Интервал переподключения |
| `FEDERATION_DISCOVERY_MODE` | manual | manual / dht / gossip |
| `FEDERATION_BOOTSTRAP_NODE` | "" | Адрес bootstrap-сервера (для DHT-режима) |
