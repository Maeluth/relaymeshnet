# L2: DHT & Service Discovery 🟡 Required

> **Required.** Не протокол, но без DHT узлы не находят друг друга, сервисы не регистрируются, контент не индексируется. Сеть технически работает, но практически бесполезна.

## Назначение

DHT (Distributed Hash Table) — распределённая хэш-таблица, заменяющая центральный
сервер имён. Хранит:
- Список peer'ов с их публичными ключами и reputation
- Сервисы (скрытые сайты) и их маршруты
- Контентные хэши → список узлов, хранящих чанк
- Order book для внутренней биржи RELAY (будет вынесен в отдельный модуль DEX)

## Выбор протокола: S/Kademlia

Kademlia — самый проверенный DHT (BitTorrent, IPFS, Ethereum).

**Проблема стандартной Kademlia в mesh**: предполагает относительно стабильную
связность и низкую latency. В mesh с LoRa это не так.

**Адаптации S/Kademlia для mesh**:

1. **Параллельные запросы** — α = 3 (отправляем запрос 3 узлам одновременно)
2. **Таймауты с бэк-оффом** — первый таймаут 2с (WiFi), второй 10с, третий 30с (LoRa)
3. **S/Kademlia** — защита от Sybil через криптографические puzzle'ы при генерации NodeID
4. **Selective storage** — не все узлы хранят все ключи, только ближайшие по XOR distance

## Self-certifying names

Каждый сервис идентифицируется хэшем публичного ключа владельца + названия:

```
ServiceID = SHA256(owner_pubkey + service_name)
```

- Два разных человека могут назвать сервис `students-wiki`
- Их ServiceID будут разными (разные pubkey)
- Репутация владельца определяет, какому доверять
- Невозможно украсть имя без кражи приватного ключа (как IPNS)

## Gossip-распространение репутации

Параллельно с DHT работает gossip-протокол для быстрого распространения
репутационной информации. Заимствован SNR-based suppression из Meshtastic.

```
Каждые 60 секунд (+ exponential jitter ±15 сек):
  1. Выбрать 3 случайных соседа
  2. Отправить им свой reputation vector (топ-50 известных узлов с score)
  3. Получить их reputation vectors
  4. Объединить (merge), обновить локальный DHT

При ретрансляции gossip:
  - SNR-based backoff: дальние узлы ретранслируют первыми
  - Ближние подавляются (suppression) → минимум дубликатов
```

Bloom-фильтры для дедупликации — не гоним одни и те же обновления повторно.

## Регистрация сервиса

```go
func RegisterService(name string, publicKey []byte, handler http.Handler) error {
    serviceID := sha256(publicKey + name)

    // 1. Подписать регистрацию
    sig := sign(serviceID, privateKey)

    // 2. Положить в DHT
    dht.Put(serviceID, &ServiceRecord{
        Name:      name,
        Owner:     publicKey,
        Signature: sig,
        Timestamp: poh.Now(),       // PoH timestamp
        Endpoints: []string{},      // не храним IP — маршрут через onion
        TTL:       24 * time.Hour,
    })

    // 3. Периодически обновлять TTL
    go refreshLoop(serviceID, 12*time.Hour)
}
```

## Discovery сервисов (каталог в WebUI)

```go
func DiscoverServices(query string) []*ServiceRecord {
    // 1. Ищем в DHT по префиксу имени
    results := dht.Search(query)

    // 2. Сортируем по reputation владельца
    sort.Slice(results, func(i, j int) bool {
        return results[i].OwnerReputation > results[j].OwnerReputation
    })

    return results
}
```

WebUI отображает:

```
Доступные сервисы:
┌──────────────────────────────────────────────────────┐
│ students-wiki     (0xABCD) ★★★★☆  12 узлов онлайн   │
│ dorm-market       (0x1234) ★★★★★   8 узлов онлайн   │
│ file-hub          (0x5678) ★★★☆☆   3 узла онлайн    │
│ lecture-notes     (0x9ABC) ★★☆☆☆   1 узел онлайн    │
└──────────────────────────────────────────────────────┘
```

## Content Provider Discovery: кто хранит чанк

Модуль `04-content-storage.md` использует `DHT.GetProviders(chunkHash)` для поиска
узлов, хранящих конкретный чанк файла. Этот раздел определяет как это работает.

### Регистрация провайдера

Когда узел сохраняет чанк (при скачивании или репликации), он регистрирует себя
как провайдер в DHT:

```go
func (n *Node) AnnounceProvider(chunkHash [32]byte) {
    // Ключ: "provider:" + hex(chunkHash)
    // Значение: список peerID узлов, хранящих этот чанк
    key := "provider:" + hex.EncodeToString(chunkHash[:])
    
    // Атомарное обновление: читаем текущий список, добавляем себя, пишем обратно
    existing := dht.Get(key)
    if !contains(existing.Providers, n.peerID) {
        existing.Providers = append(existing.Providers, n.peerID)
        existing.Timestamp = poh.Now()
        dht.Put(key, existing)
    }
}
```

### Запрос провайдеров

```go
func (n *Node) GetProviders(chunkHash [32]byte) []string {
    key := "provider:" + hex.EncodeToString(chunkHash[:])
    record := dht.Get(key)
    
    // Фильтруем: только онлайн-узлы (проверяем через ping или gossip)
    var online []string
    for _, peerID := range record.Providers {
        if n.isOnline(peerID) {
            online = append(online, peerID)
        }
    }
    return online
}
```

### TTL и очистка

- Провайдер-записи имеют TTL = 6 часов
- Узел периодически обновляет свою регистрацию (каждые 3 часа)
- Если узел офлайн > 6 часов — его запись удаляется из DHT
- При скачивании чанка узел автоматически регистрируется как провайдер

### Репликация и балансировка

Когда `len(GetProviders(chunkHash)) < REPLICATION_FACTOR` (по умолчанию 3):
1. Узел запрашивает чанк у существующего провайдера
2. Сохраняет локально
3. Регистрирует себя как провайдер
4. Репутация узла растёт (STORAGE_REWARD из `05-tokenomics.md`)

Это обеспечивает автоматическую репликацию: популярные файлы имеют много провайдеров,
редкие — минимум REPLICATION_FACTOR.

В offline-сети нет центрального сервера ключей. Решение — несколько механизмов,
от быстрых до гарантированных:

### 1. Локальный gossip (быстрый, для соседей)

При подключении к сети узел получает peer list от соседей. Peer list содержит
`{peerID, pubkey, nickname}` для всех известных узлов. Алиса ищет Боба по никнейму
в локальном peer list'е.

Ограничение: только узлы в той же подсети (те, что в gossip-радиусе).

### 2. DHT-запрос по никнейму (медленнее, глобальный)

```go
// Боб при регистрации публикует в DHT:
dht.Put("nickname:" + bob_nickname, &NicknameRecord{
    PeerID:    bob_peerID,
    PubKey:    bob_pubkey,
    Signature: sign(bob_privkey, bob_nickname),
    PoHProof:  poh.GenerateProof(...),
    TTL:       7 * 24 * time.Hour,
})

// Алиса ищет:
record := dht.Get("nickname:" + bob_nickname)
// Проверяет подпись → получает pubkey Боба
```

### 3. Внеполосный обмен (out-of-band, для первой встречи)

Два узла, которые никогда не встречались, обмениваются ключами через:
- QR-код на экране телефона (WebUI генерирует QR с peerID + pubkey)
- Bluetooth/NFC
- Физическую записку с peerID

После первого контакта ключ кэшируется локально и проверяется через DHT.

### 4. Верификация ключа

При получении pubkey через любой из механизмов, Алиса:
1. Проверяет подпись на NicknameRecord (если из DHT)
2. Проверяет peerID == SHA256(pubkey) — самосертификация
3. Проверяет PoH-доказательство (узел существует минимум N времени)
4. Проверяет reputation (gossip) — не забанен ли, не мошенник ли

Даже если злоумышленник подменит pubkey в DHT, подпись не пройдёт проверку.
Единственный способ украсть identity — украсть приватный ключ.

Каждая запись в DHT имеет TTL. Владелец должен периодически обновлять.
Если сервис упал и TTL истёк — запись удаляется из DHT.

Для отказоустойчивости: сервис может быть **зеркалирован** другими узлами.
Любой узел, имеющий полную копию контента сервиса, может зарегистрироваться
как mirror этого ServiceID и принимать запросы.

## Запрос к сервису через onion

```
Клиент → WebUI → DHT.поиск("students-wiki")
                 ↓
              ServiceID = 0xABCD...
                 ↓
         DHT.Get(ServiceID) → ServiceRecord (owner pubkey)
                 ↓
         Onion.построитьЦепь(owner, hops=3)
                 ↓
         HTTP-запрос через onion-цепь → Сервер сервиса
                 ↓
         Ответ через обратную onion-цепь → Клиент
```

Клиент не знает физического расположения сервера. Сервер не знает клиента.
Внешний наблюдатель видит только relay-трафик между случайными узлами.
