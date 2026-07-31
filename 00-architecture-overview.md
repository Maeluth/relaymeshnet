# RelayMeshNet Mesh: Architecture Overview

## Назначение

Децентрализованная оверлейная сеть, работающая **без интернета**, устойчивая к глушению
радиосигнала. Предназначена для кампусй в особых экономических зонах, где мобильная
связь и интернет недоступны или заблокированы.

Каждый роутер в комнате кампуся становится узлом сети. Узлы общаются друг с другом
через LoRa (между зданиями) и WiFi (внутри здания), образуя mesh-топологию.

## Принципы

1. **Offline-first** — сеть работает полностью автономно, интернет не требуется
2. **Onion routing** — каждый узел является потенциальным relay, анонимизируя трафик
3. **E2E encryption** — NaCl box (Curve25519 + XSalsa20-Poly1305)
4. **Content-addressed storage** — данные идентифицируются хэшем, раздаются по BitTorrent-подобной модели
5. **Self-sovereign identity** — identity = hash(public_key), никаких центральных реестров
6. **Mutual credit economy** — внутренняя токеномика на основе выполненной relay-работы

## Многослойная архитектура

```
┌─────────────────────────────────────────────────────────┐
│  L5: Application Layer                                  │
│  Чат, файлы, сайты (скрытые сервисы), SDK              │
├─────────────────────────────────────────────────────────┤
│  L4: Service Layer                                      │
│  Регистрация сервисов в DHT, discovery, хостинг        │
├─────────────────────────────────────────────────────────┤
│  L3: Content Layer                                      │
│  Content-addressed storage, erasure coding, chunking    │
├─────────────────────────────────────────────────────────┤
│  L2: Routing Layer                                      │
│  Onion routing + Kademlia DHT + PoH (Proof of History) │
├─────────────────────────────────────────────────────────┤
│  L1: Transport Layer                                    │
│  Абстракция: LoRa, WiFi mesh, TCP, (pluggable)         │
└─────────────────────────────────────────────────────────┘
```

## Два режима — один бинарник

| Режим | Флаг | Описание |
|---|---|---|
| **Client** | `--mode client` | Подключение к Host-узлу по WiFi. WebUI с чатом, каталогом сервисов, файлами. Не релеит самостоятельно — relay выполняет родительский Host. Баланс ведётся на Host-узле. |
| **Host** | `--mode host` | Полноценный узел: relay, хостинг сервисов, контент-нода. Обслуживает подключённых Client'ов. |
| **Bridge** | `--mode bridge` | Узел с интернет-доступом. Шлюз между mesh и внешним миром. Опционально. |

Одно Go-приложение компилируется под Linux (роутеры на OpenWRT) и Windows.

## Модульная документация

Легенда: 🔴 Core (протокол) | 🟡 Required (сеть неполноценна без) | 🟢 Optional (приложение/расширение)

- `01-transport-layer.md` 🔴 — физический и транспортный уровень
- `02-onion-routing.md` 🔴 — onion-маршрутизация и анонимность
- `03-dht-service-discovery.md` 🟡 — DHT, peer discovery, сервисы
- `04-content-storage.md` 🟡 — контентно-адресуемое хранилище
- `05-tokenomics.md` 🟡 — экономическая модель, токены, репутация
- `06-proof-of-history.md` 🟡 — PoH, trustless timestamping в mesh
- `07-bridge-external.md` 🟡 — мост во внешний мир, mailbox-протокол
- `08-threat-model.md` 🟡 — модель угроз и защита от атак
- `09-sdk-api.md` 🟢 — публичный SDK для разработчиков
- `10-pending-queue.md` 🟡 — pending-транзакции при partitions, модель баланса, FIFO
- `11-confirm-n-economy.md` 🟡 — двухуровневая экономика: confirm-N (бесплатно) + RELAY credits (платно)
- `12-bootstrapping.md` 🟡 — первый контакт с сетью, обнаружение соседей, верификация, защита от fake bootstrap
- `13-hybrid-economy.md` 🟡 — гибридная экономика: эмиссия (confirm-N) + transaction burn + обязательный relay
- `14-web-services.md` 🟢 — веб-сервисы поверх mesh: каталог, CDN-слой, HTTP-прокси, маршрутизация статики/динамики
- `15-devices-nodes-hosts.md` 🟡 — устройства, узлы и хосты: три типа участников, transfer, хостинг, аутентификация
- `16-crypto-identity.md` 🟡 — криптография и идентичность: X3DH, Double Ratchet, self-certifying names, защита от self-mining и spoofing
- `17-protocol-spec.md` 🔴 — спецификация протокола: wire format, onion-пакет, DHT RPC, PoR, PoH, X3DH, Confirm-N, pending state machine
- `18-versioning-compat.md` 🔴 — версионирование и совместимость: обратная совместимость, capability negotiation, три профиля узлов
- `19-link-verification.md` 🟡 — верификация линков и классы узлов: bidirectional check, node classes, link quality table
- `20-economic-attacks.md` 🟡 — экономические атаки: Sybil reset, self-mining, cross-network migration, защита через PoH age gate и ramp-up
- `21-hardware-design.md` 🟢 — продуктовый дизайн: три модели устройств (RelayStation, MeshNode, MeshStick), модульность, roadmap
- `22-deployment-params.md` 🟡 — практические параметры: частоты и закон РФ, EIRP, дальность LoRa, кросс-компиляция, ESP32+SX1276, PoE, антенны
- `23-firmware-ecosystem.md` 🟢 — экосистема прошивок: множественные разработчики, подпись Ed25519, каталог, обновление через mesh, BitTorrent-распространение
- `24-qos-multipath.md` 🟡 — QoS и multi-path: TCP/UDP-режимы, striping WiFi+LoRa, LoRa-бюджет, адаптивный выбор канала, сжатие
- `25-node-lifecycle.md` 🟡 — жизненный цикл узла: BOOTSTRAP → SANDBOX → RAMP-UP → ACTIVE → TRUSTED, vouch-механика
- `26-identity-export.md` 🟡 — экспорт/импорт identity: зашифрованный бэкап, cross-device sync, физический перенос
- `27-cold-restart.md` 🟡 — холодный рестарт сети: фазы восстановления, частичный/полный cold restart, новый старт
- `28-sovereignty-clans.md` 🟡 — суверенитеты и кланы: социальный слой доверия, белые/серые/чёрные зоны, бан-листы, добровольная федерация
