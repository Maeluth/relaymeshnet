# Dependency Tree

> Математическая метафора: аксиомы → примитивы → формулы → теоремы → приложения.
> Изменение Слоя N требует ревизии всех слоёв ≥ N, которые от него зависят.

---

## Слой 0: Аксиомы (реальность — не модули)

Менять нельзя. Новые появляются только если законы физики или права изменились.

```
ФИЗИКА:
  • Частоты: 868.7-869.2 MHz (EU SRD, Россия)
  • Мощность: ≤25 mW (14 dBm) без регистрации, 26-100 mW требует РЭС
  • Duty cycle: 1% (≈36 секунд передачи в час)
  • Дальность LoRa: 1-5 км (город), 10-15 км (открытое пространство)
  • SF7-SF12: bandwidth 125-500 kHz, spreading factor
  • Friis path loss: L = 20log(d) + 20log(f) - 147.55
  • SNR = Ptx - L - NoiseFloor, PSR = f(SNR) ступенчатая функция
  • ESP32: 240 MHz, 520 KB SRAM, 4 MB Flash, SPI к SX1276
  • OpenWRT: MIPS/ARM, 64-512 MB RAM, WiFi 2.4 GHz (802.11n/s)

МАТЕМАТИКА:
  • Ed25519: подписи, 32-байт ключи, 64-байт подписи
  • X25519: DH-обмен, 32-байт ключи, Curve25519
  • SHA-256: хэширование, PoH, PeerID
  • AES-256-GCM: симметричное шифрование (AEAD)
  • NaCl box: Curve25519 + XSalsa20-Poly1305
  • Base58: кодирование PeerID для отображения
  • XOR-метрика: distance в Kademlia DHT (160-бит)
  • EWMA: exponential weighted moving average, half-life = 7 дней

ПРАВО (РФ):
  • 868.7-869.2 MHz: нелицензируемый диапазон (SRD), ≤25 mW без регистрации
  • >25 mW: регистрация РЭС обязательна
  • Шифрование: законодательство зависит от юрисдикции зоны
  • Электропитание: роутеры от сети/аккумуляторов

Зависит от: ничего (это реальность)
Требуется для: ВСЕГО
```

---

## Слой 1: Базовые примитивы 🔴

Построены **только** на Слое 0. Менять можно, но с осторожностью — замена ломает всё выше.

| Модуль | Зависит от | Требуется для | Тип |
|---|---|---|---|
| **AXIOMS** (не модуль) | — | Все модули | Аксиомы |
| `34-lora-driver.md` 🔴 | AXIOMS | 01, 08, 09, 24, 32 | 🔴 Core |
| `29-hal-spec.md` 🟡 | AXIOMS | 30, 31, 37, 40 | 🟡 Required |
| `01-transport-layer.md` 🔴 | AXIOMS | 02, 08, 09, 17, 19, 24 | 🔴 Core |
| `16-crypto-identity.md` 🟡 | AXIOMS | 02, 03, 05, 06, 17, 23, 28, 31, 40 | 🟡 Required |
| `02-onion-routing.md` 🔴 | 16 | 17, 24, 37 | 🔴 Core |
| `08-threat-model.md` 🟡 | 16, 02 | 20, 23, 27, 37, 40 | 🟡 Required |

### Граф зависимостей Слоя 1

```
AXIOMS
  ├── 34-lora-driver (serial v2, CRC16, fragmentation, C-прошивка)
  │     └── 01-transport (абстракция над LoRa/WiFi)
  ├── 29-hal-spec (hardware detection: CPU/RAM/Flash/radios)
  │     └── 30-device-profile (auto-role: relay/node/bridge)
  └── 16-crypto-identity (Ed25519, X25519, X3DH, Double Ratchet)
        ├── 02-onion-routing (circuit building, cover traffic)
        └── 08-threat-model (vectors, defenses, trust levels)

34 + 01 + 02 + 16 + 08 = ПРИМИТИВЫ: на этом можно построить любую mesh-сеть
```

---

## Слой 2: Протоколы 🔴🟡

Построены на Слое 1. Определяют логику сети. Без них — просто радио в эфире.

| Модуль | Зависит от | Требуется для | Тип |
|---|---|---|---|
| `17-protocol-spec.md` 🔴 | 01, 02, 16 | всё на Слое 2+3+4 | 🔴 Core |
| `18-versioning-compat.md` 🔴 | 17 | 23, 31, 32 | 🔴 Core |
| `03-dht-service-discovery.md` 🟡 | 17, 16, 08 | 04, 12, 14, 35, 38 | 🟡 Required |
| `35-gossip-dht.md` 🟡 | 03, 16 | 04, 12, 14, 38 | 🟡 Required |
| `12-bootstrapping.md` 🟡 | 03, 19 | 22, 25, 27 | 🟡 Required |
| `19-link-verification.md` 🟡 | 01 | 12, 14, 24, 25 | 🟡 Required |
| `08-threat-model.md` 🟡 | на Слое 1 | 20, 23, 27, 37, 40 | 🟡 Required |
| `06-proof-of-history.md` 🟡 | 16 | 05, 16, 17, 36 | 🟡 Required |
| `36-poh-sync.md` 🟡 | 06 | 05, 16, 17 | 🟡 Required |
| `37-adaptive-onion.md` 🟡 | 02, 29 | 24, 40 | 🟡 Required |
| `40-multi-identity.md` 🟢 | 16, 29, 25, 28 | — (приложение) | 🟢 Optional |

### Граф зависимостей Слоя 2

```
Слой 1 (примитивы)
  ├── 17-protocol-spec (wire format, onion packet, DHT RPC, PoR, PoH, X3DH, confirm-N)
  │     └── 18-versioning-compat (cap negotiation, 3 профиля)
  │           ├── 31-ota-updates
  │           └── 32-data-migration
  ├── 03-dht-service-discovery (Kademlia, self-certifying names, service records)
  │     ├── 35-gossip-dht (bloom filters, delta sync — альтернатива Kademlia)
  │     ├── 12-bootstrapping (first contact, peer discovery, anti-fake)
  │     └── 04-content-storage
  ├── 19-link-verification (bidirectional check, node classes, link quality)
  │     └── 22-deployment-params (практические параметры)
  ├── 06-proof-of-history (SHA256 chain, checkpoint storage)
  │     └── 36-poh-sync (cross-reference, causal ordering, offset correction)
  ├── 37-adaptive-onion (Full/Medium/Light/Lite по RAM/каналу/CPU)
  └── 40-multi-identity (несколько ID на устройстве)
```

---

## Слой 3: Сервисы 🟡🟢

Построены на Слое 2. Это то что «видит» пользователь и разработчик.

| Модуль | Зависит от | Требуется для | Тип |
|---|---|---|---|
| `05-tokenomics.md` 🟡 | 16, 06, 17, 36 | 07, 10, 11, 13, 14, 20, 21, 28 | 🟡 Required |
| `13-hybrid-economy.md` 🟡 | 05, 11 | 07, 10, 14, 20, 21 | 🟡 Required |
| `11-confirm-n-economy.md` 🟡 | 05, 17 | 13, 14 | 🟡 Required |
| `10-pending-queue.md` 🟡 | 05, 17 | 13, 14 | 🟡 Required |
| `07-bridge-external.md` 🟡 | 02, 03, 05, 17 | 14, 38 | 🟡 Required |
| `04-content-storage.md` 🟡 | 03, 16, 35 | 14, 24 | 🟡 Required |
| `24-qos-multipath.md` 🟡 | 01, 02, 19, 37 | 14, 22, 32 | 🟡 Required |
| `20-economic-attacks.md` 🟡 | 05, 08, 13 | 22, 25, 28 | 🟡 Required |
| `25-node-lifecycle.md` 🟡 | 06, 12, 19, 20 | 26, 27, 28, 40 | 🟡 Required |
| `22-deployment-params.md` 🟡 | 19, 21, 24 | — (справочник) | 🟡 Required |
| `30-device-profile.md` 🟡 | 29, 40 | 21, 31 | 🟡 Required |
| `31-ota-updates.md` 🟡 | 17, 18, 23, 30 | 32 | 🟡 Required |
| `32-data-migration.md` 🟡 | 17, 18 | 33 | 🟡 Required |
| `33-monitoring.md` 🟡 | 01, 19, 24 | — (диагностика) | 🟡 Required |
| `26-identity-export.md` 🟡 | 16, 25 | 27 | 🟡 Required |
| `27-cold-restart.md` 🟡 | 03, 12, 25, 26 | 28 | 🟡 Required |
| `28-sovereignty-clans.md` 🟡 | 25, 27 | 40 | 🟡 Required |
| `38-intermesh-federation.md` 🟡 | 03, 07, 35 | — (расширение) | 🟡 Required |

### Граф зависимостей Слоя 3

```
Слой 2 (протоколы)
  ├── ЭКОНОМИКА:
  │   ├── 05-tokenomics ──→ 13-hybrid-economy
  │   │     ├── 11-confirm-n-economy
  │   │     ├── 10-pending-queue
  │   │     ├── 07-bridge-external
  │   │     └── 20-economic-attacks
  │   └── 13 ──→ 14-web-services
  │
  ├── ИНФРАСТРУКТУРА:
  │   ├── 04-content-storage ──→ 24-qos-multipath
  │   ├── 30-device-profile ──→ 31-ota-updates ──→ 32-data-migration
  │   ├── 22-deployment-params
  │   └── 33-monitoring
  │
  ├── ЖИЗНЕННЫЙ ЦИКЛ:
  │   ├── 25-node-lifecycle ──→ 26-identity-export ──→ 27-cold-restart
  │   └── 27 ──→ 28-sovereignty-clans
  │
  └── МЕЖСЕТЕВОЕ:
      └── 38-intermesh-federation
```

---

## Слой 4: Приложения 🟢

Построены на Слое 3. Опциональны. Менять свободно — протокол не затронут.

| Модуль | Зависит от | Требуется для | Тип |
|---|---|---|---|
| `09-sdk-api.md` 🟢 | 17, 14 | external dev | 🟢 Optional |
| `14-web-services.md` 🟢 | 03, 04, 05, 07, 10, 11, 13, 24 | 09, 15, 21 | 🟢 Optional |
| `15-devices-nodes-hosts.md` 🟡 | 14, 17, 25 | 21 | 🟡 Required |
| `21-hardware-design.md` 🟢 | 15, 22, 30 | — (продукт) | 🟢 Optional |
| `23-firmware-ecosystem.md` 🟢 | 18, 31 | — (экосистема) | 🟢 Optional |
| `39-network-evolution.md` 🟢 | 05, 13, 28, 38 | — (аналитика) | 🟢 Optional |
| `40-multi-identity.md` 🟢 | на Слое 2 | — (приложение) | 🟢 Optional |

### Граф зависимостей Слоя 4

```
Слой 3 (сервисы)
  ├── 14-web-services ──→ 09-sdk-api
  │     └── 15-devices-nodes-hosts ──→ 21-hardware-design
  ├── 23-firmware-ecosystem
  ├── 39-network-evolution (аналитический, ни на что не влияет)
  └── 40-multi-identity (изолирован, зависит только от Слоя 2)
```

---

## Сводный DAG (Directed Acyclic Graph)

```
                          ┌───────────────────────────┐
                          │     СЛОЙ 0: АКСИОМЫ       │
                          │  (физика, математика,     │
                          │   право — не модули)      │
                          └──────────┬────────────────┘
                                     │
        ┌────────────────────────────┼────────────────────────────┐
        │                            │                            │
        ▼                            ▼                            ▼
┌───────────────┐          ┌───────────────┐          ┌───────────────┐
│ 34-lora-driver │          │  29-hal-spec  │          │ 16-crypto-id   │
│      🔴        │          │      🟡       │          │      🟡        │
└───────┬───────┘          └───────┬───────┘          └───────┬───────┘
        │                          │                          │
        ▼                          ▼                          ▼
┌───────────────┐          ┌───────────────┐          ┌───────────────┐
│  01-transport │          │30-dev-profile │          │02-onion-routing│
│      🔴        │          │      🟡       │          │      🔴        │
└───────┬───────┘          └───────┬───────┘          └───────┬───────┘
        │                          │                          │
        └──────────────────────────┼──────────────────────────┘
                                   │
                                   ▼
                    ┌─────────────────────────────┐
                    │      СЛОЙ 2: ПРОТОКОЛЫ      │
                    │                              │
                    │  17-protocol-spec 🔴         │
                    │  18-versioning-compat 🔴     │
                    │  03-dht-service-discovery 🟡  │
                    │  35-gossip-dht 🟡            │
                    │  12-bootstrapping 🟡         │
                    │  19-link-verification 🟡     │
                    │  06-proof-of-history 🟡      │
                    │  36-poh-sync 🟡              │
                    │  37-adaptive-onion 🟡        │
                    │  40-multi-identity 🟢        │
                    └──────────────┬──────────────┘
                                   │
                                   ▼
                    ┌─────────────────────────────┐
                    │      СЛОЙ 3: СЕРВИСЫ        │
                    │                              │
                    │  05-tokenomics 🟡            │
                    │  13-hybrid-economy 🟡        │
                    │  11-confirm-n-economy 🟡     │
                    │  10-pending-queue 🟡         │
                    │  07-bridge-external 🟡       │
                    │  04-content-storage 🟡       │
                    │  24-qos-multipath 🟡         │
                    │  20-economic-attacks 🟡      │
                    │  25-node-lifecycle 🟡        │
                    │  22-deployment-params 🟡     │
                    │  31-ota-updates 🟡           │
                    │  32-data-migration 🟡        │
                    │  33-monitoring 🟡            │
                    │  26-identity-export 🟡       │
                    │  27-cold-restart 🟡          │
                    │  28-sovereignty-clans 🟡     │
                    │  38-intermesh-federation 🟡  │
                    └──────────────┬──────────────┘
                                   │
                                   ▼
                    ┌─────────────────────────────┐
                    │     СЛОЙ 4: ПРИЛОЖЕНИЯ      │
                    │                              │
                    │  09-sdk-api 🟢               │
                    │  14-web-services 🟢          │
                    │  15-devices-nodes-hosts 🟡   │
                    │  21-hardware-design 🟢       │
                    │  23-firmware-ecosystem 🟢    │
                    │  39-network-evolution 🟢     │
                    └─────────────────────────────┘
```

---

## Change Impact Matrix

Что ломается при изменении модуля X:

| Меняемый модуль | Затрагивает |
|---|---|
| **Слой 0 (аксиомы)** | **ВСЕ** модули 1-39. Полный редизайн протокола. |
| **34-lora-driver** 🔴 | 01, 09, 24, 32, симуляция |
| **29-hal-spec** 🟡 | 30, 31, 37, 40 |
| **16-crypto-identity** 🟡 | 02, 03, 05, 06, 17, 23, 28, 31, 40, вся симуляция |
| **02-onion-routing** 🔴 | 17, 24, 37, симуляция |
| **17-protocol-spec** 🔴 | ВСЕ модули Слоёв 3-4 |
| **05-tokenomics** 🟡 | 07, 10, 11, 13, 14, 20, 21, 28 |
| **13-hybrid-economy** 🟡 | 07, 10, 14, 20, 21 |
| **25-node-lifecycle** 🟡 | 26, 27, 28, 40 |
| **28-sovereignty-clans** 🟡 | 40 |
| **39-network-evolution** 🟢 | ничего (чисто аналитический модуль) |
| **40-multi-identity** 🟢 | ничего (зависит только от Слоя 2 вниз) |

---

## Build Order (порядок реализации)

Приоритет реализации на основе зависимостей:

```
Фаза 1 (MVP): Слой 0 + Слой 1 (можно менять почти без последствий)
  → AXIOMS, 34-lora-driver, 29-hal-spec,
    01-transport, 16-crypto-identity, 02-onion-routing

Фаза 2: Слой 2 (протоколы)
  → 17-protocol-spec, 18-versioning, 03-dht, 12-bootstrapping,
    19-link-verification, 06-poh, 35-gossip-dht, 37-adaptive-onion

Фаза 3: Слой 3 (сервисы)
  → 05-tokenomics, 13-economy, 04-storage, 24-qos,
    25-lifecycle, 30-profile, 31-ota, 28-sovereignty, 38-federation

Фаза 4: Слой 4 (приложения)
  → 14-web-services, 09-sdk, 39-evolution, 40-multi-identity
```

Каждая фаза требует завершения **всех** зависимостей предыдущей фазы.

---

## Классификация по важности для MVP

```
Что НЕОБХОДИМО для запуска первого узла (MVP):
  ✅ AXIOMS (физика)
  ✅ 34-lora-driver (ESP32 ↔ роутер)
  ✅ 29-hal-spec (автоопределение железа)
  ✅ 01-transport (LoRa + WiFi)
  ✅ 16-crypto-identity (ключи, X3DH, Double Ratchet)
  
  → Достаточно для: отправить первое зашифрованное сообщение по LoRa

Что нужно для relay-сети из 3+ узлов:
  ✅ 02-onion-routing (маршрутизация, cover traffic)
  ✅ 17-protocol-spec (wire format, PoR)
  ✅ 06-proof-of-history (timestamps)
  ✅ 12-bootstrapping (первый контакт с сетью)
  ✅ 19-link-verification (качество соединений)
  
  → Достаточно для: три узла релеят трафик друг другу

Что нужно для полноценной экономики:
  ✅ 05-tokenomics
  ✅ 13-hybrid-economy
  ✅ 11-confirm-n-economy
  ✅ 20-economic-attacks (защита)
  
  → Достаточно для: RELAY циркулируют, узлы зарабатывают и тратят

Что нужно для масштабирования:
  ✅ 03-dht-service-discovery (поиск)
  ✅ 35-gossip-dht (эффективная синхронизация)
  ✅ 28-sovereignty-clans (социальный слой)
  ✅ 25-node-lifecycle (жизненный цикл)
  
  → Достаточно для: 100+ узлов с кланами и DHT

Что можно отложить:
  ⏸ 09-sdk-api (SDK для разработчиков)
  ⏸ 14-web-services (.mesh сайты)
  ⏸ 21-hardware-design (продуктовый дизайн)
  ⏸ 23-firmware-ecosystem (сторонние прошивки)
  ⏸ 38-intermesh-federation (другие mesh'и)
  ⏸ 39-network-evolution (аналитика)
  ⏸ 40-multi-identity (свобода пользователя)
```

## Примечания

- **Слой 4 модули** могут писаться параллельно с Слоем 3 — они изолированы
- **Слой 2 модули** могут писаться параллельно внутри слоя — они независимы друг от друга
- **Слой 3 модули** имеют сильные перекрёстные зависимости — нужна очерёдность
- **40-multi-identity** изолирован: зависит от Слоя 2, не требуется никем — идеальный кандидат для параллельной разработки
- **Симуляция** (`simulation/`) реализует фрагменты всех слоёв в упрощённой модели — она НЕ заменяет production-ноду, но валидирует архитектуру
