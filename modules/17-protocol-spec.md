# Protocol Specification 🔴 Core

> **Core.** Это и есть протокол. Wire format, onion-пакет, DHT RPC, PoR, PoH, X3DH, Confirm-N, state machine. Без этого спецификация сети неполна — нельзя написать совместимую ноду.

Спецификация протокола RelayMeshNet (RMN) — мост между документацией и реализацией.
Достаточно для написания совместимой ноды на любом языке.

## 1. Node Identity

```
PeerID = SHA256(Ed25519_public_key)[0:16]  // 16 байт, hex-encoded = 32 символа

Генерация:
  1. Создать Ed25519 keypair
  2. PeerID = hex(SHA256(public_key)[:16])
  3. Проверка: SHA256(public_key)[:16] == PeerID

Самосертификация:
  Любой узел может проверить: предъявленный public_key → SHA256 → первые 16 байт == PeerID
  Без доверенного центра.
```

## 2. Transport Layer

### Wire Format

```
┌──────────┬──────────┬──────────┬──────────┬───────────────┐
│ Magic    │ Version  │ Type     │ Length   │ Payload       │
│ 4 bytes  │ 2 bytes  │ 2 bytes  │ 2 bytes  │ 0..65535      │
│ "RMN\x01"│          │          │ big-end  │               │
└──────────┴──────────┴──────────┴──────────┴───────────────┘
```

### Transport Interface

```go
type Transport interface {
    Send(peerID string, payload []byte) error
    Broadcast(payload []byte) error
    Recv() <-chan *Packet
    Neighbors() []NeighborInfo
}

type Packet struct {
    FromPeerID string
    Payload    []byte
    RSSI       float64
    SNR        float64
    Channel    string // "wifi", "lora_sf7", "lora_sf12"
}

type NeighborInfo struct {
    PeerID    string
    RSSI      float64
    LastSeen  time.Time
    Channel   string
}
```

### LoRa Fragmentation

```
Если payload > MaxPayload(SF):
  1. Разбить payload на фрагменты по MaxPayload
  2. Каждый фрагмент: [FragID:2][TotalFrags:2][Data:...]
  3. Получатель собирает: ждёт все TotalFrags фрагментов
  4. Timeout сборки: 60 секунд → discard
```

## 3. Onion Packet

### Структура

```
OnionPacket (N слоёв):
  Layer[0]: encrypted_for(hop[0], inner=[next_hop || Layer[1]])
  Layer[1]: encrypted_for(hop[1], inner=[next_hop || Layer[2]])
  ...
  Layer[N-1]: encrypted_for(hop[N-1], inner=[FLAG_FINAL || plaintext])

Шифрование: NaCl box.Seal (Curve25519 + XSalsa20-Poly1305)
Overhead: 24 (nonce) + 16 (MAC) = 40 байт на слой

Размер onion-пакета для M байт plaintext через H hop'ов:
  Size = M + H*40 + (H-1)*32 + 1
  Для H=3, M=200: 200 + 120 + 64 + 1 = 385 байт
```

### Hop Data (после расшифровки слоя)

```
┌──────────┬──────────────────────────────┐
│ Flag     │ Payload                      │
│ 1 byte   │ ...                          │
├──────────┴──────────────────────────────┤
│ Flag = 0x00: FINAL, Payload = plaintext │
│ Flag = 0x01: RELAY, Payload =          │
│   [NextHop:32][InnerOnion:...]          │
└─────────────────────────────────────────┘
```

### Circuit Building

```
BuildCircuit(targets []PeerID, hops int) → OnionPacket:
  1. Выбрать H случайных relay-узлов из DHT (reputation-weighted)
  2. Построить onion рекурсивно изнутри наружу
  3. Самый внутренний слой: FLAG_FINAL + plaintext, encrypted for target
  4. Каждый следующий: FLAG_RELAY + next_hop + inner, encrypted for hop
  5. Вернуть готовый OnionPacket

UnwrapLayer(packet []byte, privateKey [32]byte) → (flag byte, payload []byte):
  1. Попытаться box.Open с privateKey
  2. Если ошибка → пакет не нам, relay as-is
  3. Если успех → прочитать flag, вернуть payload
```

## 4. DHT (S/Kademlia)

### Message Types

```go
type DHTMessage struct {
    Type      string        // "ping", "store", "find_node", "find_value"
    SenderID  string        // PeerID отправителя
    Nonce     uint64        // против replay
    Signature []byte        // Ed25519 подпись над Type+SenderID+Nonce+Payload
    Payload   []byte        // зависит от Type
}
```

### Kademlia RPC

```
PING:
  Request:  {SenderID}
  Response: {PeerID, PubKey, Reputation, PoHProof}

STORE:
  Request:  {Key[32], Value, TTL, Signature}
  Response: {Ack}

FIND_NODE:
  Request:  {TargetID[16]}  // 16-байтный XOR target
  Response: {Nodes: [{PeerID, PubKey, Address}]}  // K ближайших

FIND_VALUE:
  Request:  {Key[32]}
  Response: {Value, TTL, Signature} или {Nodes} (если не храним)
```

### Self-Certifying Names

```
NicknameRecord:
  Nickname  string
  PeerID    [16]byte
  PubKey    [32]byte
  Signature Ed25519(PeerID || Nickname)  // подпись приватным ключом
  PoHProof  []byte
  TTL       uint64

Публикация:  dht.Store("nick:" + nickname, record)
Поиск:       dht.FindValue("nick:" + nickname) → record
Проверка:    SHA256(pubkey)[:16] == PeerID && Ed25519_Verify(pubkey, record)
```

### Service Record

```
ServiceRecord:
  Name      string
  Owner     [32]byte  // pubkey владельца
  ServiceID [32]byte  // SHA256(owner_pubkey || name)
  Files     map[string][32]byte  // path → ChunkHash статики
  Signature Ed25519(owner_priv, ServiceID || hash(Files))
  PoHProof  []byte
  TTL       uint64

Публикация: dht.Store("svc:" + hex(ServiceID), record)
Поиск:      dht.FindValue("svc:" + hex(ServiceID)) → record
```

## 5. Proof of Relay (PoR)

### Receipt Format

```go
type PoRReceipt struct {
    RelayPeerID   [16]byte  // кто релеил
    PacketHash    [32]byte  // SHA256(onion_packet)
    BytesRelayed  uint64    // объём переданных данных
    PoHProof      []byte    // доказательство времени
    Signature     []byte    // Ed25519 подпись следующего hop'а
}
```

### Протокол

```
1. Relay-узел релеит пакет nextHop'у
2. Relay-узел записывает событие в PoH-stream
3. NextHop подписывает PoRReceipt своим приватным ключом
4. NextHop отправляет PoRReceipt обратно relay-узлу
5. Relay-узел сохраняет PoRReceipt как доказательство работы

Проверка третьей стороной:
  1. Ed25519_Verify(nextHop_pubkey, receipt.Signature, receipt)
  2. PoH_Verify(receipt.PoHProof) — время доказано
  3. SHA256(ожидаемый_пакет) == receipt.PacketHash — пакет тот самый

Вес PoR в репутации:
  reputation += BytesRelayed / 1024.0 * age_factor
  age_factor = exp(-(now - PoH_timestamp) / (7 * 86400))
```

## 6. Proof of History (PoH)

### Генерация

```go
type PoHGenerator struct {
    seed      [32]byte   // SHA256(pubkey || startup_time)
    lastHash  [32]byte
    tickIndex uint64
}

func (p *PoHGenerator) Tick() [32]byte {
    p.lastHash = SHA256(p.lastHash)
    p.tickIndex++
    return p.lastHash
}

func (p *PoHGenerator) RecordEvent(eventType string, dataHash [32]byte) PoHRecord {
    tick := p.Tick()
    return PoHRecord{
        TickIndex: p.tickIndex,
        TickHash:  tick,
        EventType: eventType,
        DataHash:  dataHash,
        Signature: Ed25519_Sign(privKey, tick || dataHash),
    }
}
```

### Хранение (Checkpoint-модель)

```
Каждые 10 минут (5000 тиков): сохранить checkpoint = {tickIndex, tickHash}
Каждое событие: сохранить PoHRecord
Старые события (30+ дней): агрегировать в reputation score, удалить

Размер: ~105 KB/день, ~3 MB/месяц
```

### Верификация

```go
func VerifyPoHRecord(record PoHRecord, pubKey [32]byte, checkpoint PoHCheckpoint) bool {
    // 1. Проверить подпись
    if !Ed25519_Verify(pubKey, record.Signature, record.TickHash || record.DataHash) {
        return false
    }
    // 2. Пересчитать цепочку от checkpoint до record.TickIndex
    hash := checkpoint.TickHash
    for i := checkpoint.TickIndex + 1; i <= record.TickIndex; i++ {
        hash = SHA256(hash)
    }
    return hash == record.TickHash
}
```

## 7. X3DH + Double Ratchet

### Pre-Key Bundle (публикуется в DHT)

```go
type PreKeyBundle struct {
    IdentityKey   [32]byte  // Ed25519 (долгосрочный)
    SignedPreKey  [32]byte  // Curve25519 (среднесрочный, ротация ~неделя)
    SignedPreKeySig []byte  // Ed25519 подпись IdentityKey над SignedPreKey
    OneTimePreKeys [][32]byte  // Curve25519 (одноразовые, пополняются)
    PoHProof      []byte
}
```

### X3DH Handshake

```
Alice → Bob (первое сообщение):

1. Alice получает PreKeyBundle Боба из DHT
2. Alice генерирует ephemeral Curve25519 keypair (EK_A)
3. Alice вычисляет:
   DH1 = DH(IdentityKey_A_priv, SignedPreKey_B_pub)
   DH2 = DH(EK_A_priv, IdentityKey_B_pub)
   DH3 = DH(EK_A_priv, SignedPreKey_B_pub)
   DH4 = DH(EK_A_priv, OneTimePreKey_B_pub)  // если есть
   SK = KDF(DH1 || DH2 || DH3 || DH4)
4. Alice шифрует начальное сообщение ключом SK
5. Alice отправляет: {IdentityKey_A_pub, EK_A_pub, OneTimePreKey_ID, ciphertext}

Bob при получении:
1. Вычисляет те же DH, получает SK
2. Расшифровывает начальное сообщение
3. Удаляет использованный OneTimePreKey из DHT
4. Начинает Double Ratchet
```

### Double Ratchet

```
После X3DH, каждая сторона ведёт:
  - Root Key (обновляется через DH)
  - Sending Chain Key (для отправки)
  - Receiving Chain Key (для получения)
  - Message Number (начинается с 0)

Отправка сообщения:
  1. Если Message Number == 0 в sending chain:
     → Сгенерировать новый DH keypair
     → DH_ratchet с receiving key
     → Обновить Root Key
  2. Message Key = KDF(Sending Chain Key)
  3. Sending Chain Key = KDF(Message Key)
  4. Ciphertext = AEAD_Encrypt(Message Key, plaintext, AD)
  5. Message Number++
  6. Отправить {DH_pub (если новый), Message Number, ciphertext}

Получение сообщения:
  1. Если сообщение содержит новый DH_pub:
     → DH_ratchet, обновить Root Key, Receiving Chain Key
  2. Пропустить вперёд по Receiving Chain Key до Message Number
     (если сообщения пришли не по порядку)
  3. Message Key = KDF(Receiving Chain Key)
  4. Receiving Chain Key = KDF(Message Key)
  5. Plaintext = AEAD_Decrypt(Message Key, ciphertext, AD)
```

## 8. Confirm-N Protocol

```
Отправитель хочет отправить текст (≤ 2 KB):

1. Проверить relay-очередь:
   if queue.empty:
     N = 0 (пропускаем confirm)
   else:
     N = max(2, ceil(hops × message_bytes / 512))

2. Для i = 0..N-1:
   packet = queue.pop()
   relay(packet)           // релеим чужой пакет
   emit PoRReceipt         // получаем квитанцию

3. EMISSION = N × 512 / 1024 × EMISSION_RATE
   balance += EMISSION

4. Отправить своё сообщение через onion-цепь

Если N = 0:
  EMISSION = 0
  Отправить своё сообщение (бесплатно)
```

## 9. Pending Transaction

### State Machine

```
                    ┌─────────────┐
          create    │   PENDING   │  receiver reachable
    ───────────────→│             │──────────────────→ COMPLETED
                    │ retry 5min  │
                    └──────┬──────┘
                           │ TTL expired (24h)
                           │ OR balance insufficient
                           ▼
                    ┌─────────────┐
                    │  CANCELLED  │
                    └─────────────┘
```

### Структура

```go
type PendingTx struct {
    ID          [32]byte  // SHA256(Sender || Receiver || Amount || Nonce)
    Sender      [16]byte
    Receiver    [16]byte
    Amount      uint64    // RELAY credits × 100 (целые)
    CreatedAt   uint64    // Unix timestamp
    TTL         uint64    // секунды (по умолчанию 86400)
    Status      uint8     // 0=pending, 1=completed, 2=cancelled, 3=expired
    Signature   []byte    // Ed25519 подпись отправителя
}
```

### Логика завершения

```
При ретрае (каждые 5 минут):
  1. Проверить TTL: now - CreatedAt > TTL → expire (возврат средств)
  2. Проверить доступность receiver:
     dht.FindNode(receiver) → online?
  3. Если online:
     path = FindPath(self, receiver)
     if path.len >= 2:
       if amount <= balance + credit_limit:
         balance -= amount
         locked -= amount
         status = COMPLETED
         relay-узлам: RELAY_REWARD × amount / path.relayCount
       else:
         status = CANCELLED (недостаточно средств)
         locked -= amount
  4. Если нет → остаёмся в PENDING
```

## 10. Message Types Summary

| Type | Purpose | Transport | Encryption |
|---|---|---|---|
| `MSG_CHAT` | Текстовое сообщение | Onion, 1-3 hop | NaCl box (E2E) |
| `MSG_FILE` | Файл > 2 KB | Onion, 1-3 hop | NaCl box (E2E) |
| `MSG_CONFIRM` | Confirm-N relay | Direct | Прозрачно для relay |
| `MSG_PING` | Проверка цепи | Onion/direct | — |
| `MSG_PONG` | Ответ на ping | Onion/direct | — |
| `MSG_POR` | PoR квитанция | Direct | Подписано |
| `MSG_TRANSFER` | Перевод RELAY | Onion, 3 hop | X3DH |
| `MSG_DHT_*` | DHT запросы | Direct | Подписано |
| `MSG_GOSSIP` | Reputation обмен | Direct/broadcast | Подписано |
| `MSG_COVER` | Cover traffic | Onion, loop | Неотличим от real |
