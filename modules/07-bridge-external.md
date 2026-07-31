# Bridge: Внешний мир 🟡 Required

> **Required.** Без bridge сеть навсегда изолирована. Технически mesh работает, но без выхода в интернет это интранет, а не альтернативный интернет.

## Проблема

Mesh-сеть изолирована. Некоторые узлы (bridge nodes) имеют эпизодический
доступ к интернету. Как доставить сообщение извне внутрь mesh, сохраняя
анонимность получателя?

## Архитектура Mailbox

```
Алиса (интернет)          Mesh (без интернета)            Боб (внутри mesh)
     │                          │                              │
     │──(1) encrypt for──→  [Bridge] ──(2) put──→ [DHT]       │
     │   mailbox_key          │            mailbox_msg         │
     │                        │                               │
     │                        │         [DHT] ←──(3) poll ────│
     │                        │           │        (через      │
     │                        │     mailbox_msg    onion-цепь) │
     │                        │           │                    │
     │                        │           └──(4) deliver────→ │
```

## Mailbox Key

Боб публикует mailbox key в DHT:

```go
type MailboxRecord struct {
    Owner       string    // peerID Боба (не раскрывается внешним)
    MailboxKey  [32]byte  // одноразовый Curve25519 pubkey для шифрования внешних сообщений
    BridgeHint  string    // подсказка: "ищи bridge в корпусе А" (опционально)
    NextKeyHash [32]byte  // хэш следующего ключа (для ротации)
    PoHProof    []byte    // доказательство времени публикации
    TTL         int64     // срок действия ключа (обычно 1 час)
}
```

- Внешний мир (Алиса) видит только: шифрует сообщение MailboxKey'ом
- Алиса не знает, кто такой Боб внутри mesh
- Bridge не знает содержимого (оно зашифровано для Боба)
- Боб видит: "есть сообщение для mailbox_key", расшифровывает

## Доставка через Bridge

```go
// Bridge узел
func (b *Bridge) HandleExternalMessage(msg *ExternalMessage) {
    // 1. Найти mailbox в DHT
    record := b.dht.Get("mailbox:" + msg.MailboxKeyHash)

    // 2. Положить зашифрованное сообщение в DHT
    b.dht.Put("mailbox_msg:" + msg.MailboxKeyHash, &MailboxMessage{
        Ciphertext: msg.Ciphertext,
        FromBridge: b.peerID,
        Timestamp:  b.poh.Now(),
    })

    // 3. Bridge получает RELAY credits за доставку
    b.addCredits(BRIDGE_REWARD)
}
```

### BRIDGE_REWARD: экономика bridge-узлов

Bridge-узел выполняет двойную работу:
1. **Внутри mesh**: релеит трафик как обычный узел → получает RELAY_REWARD
2. **Мост наружу**: доставляет сообщения из интернета → получает BRIDGE_REWARD

```
BRIDGE_REWARD = RELAY_REWARD × BRIDGE_MULTIPLIER

Параметры:
  RELAY_REWARD = 1 credit / KB (стандартная relay-оплата)
  BRIDGE_MULTIPLIER = 2.0 (bridge делает дополнительную работу)

Итого: BRIDGE_REWARD = 2 credits / KB доставленного внешнего сообщения
```

**Почему множитель 2.0**:
- Bridge платит за интернет (ISP)
- Bridge рискует (администрация может заметить)
- Bridge выполняет дополнительную работу (HTTP-проксирование, DHT-записи)
- Стимул держать bridge онлайн 24/7

**Пример**:
- Алиса (внешняя) отправляет Бобу сообщение 5 KB
- Bridge доставляет: 5 KB × 2.0 = 10 credits
- Bridge также релеит через mesh (3 hop'а): 5 KB × 3 × 1.0 = 15 credits
- Итого bridge зарабатывает: 10 + 15 = 25 credits

**Конкуренция bridge'ей**:
- Несколько bridge'ей → конкуренция по цене для внешних пользователей
- Bridge с высокой репутацией → больше доверия внутри mesh
- Bridge с низкой задержкой → больше внешних клиентов

## Опрос Mailbox Бобом

```go
// Боб — внутри mesh
func (bob *Node) PollMailbox() {
    // 1. Через onion-цепь запрашиваем DHT
    circuit := bob.buildCircuit()  // случайная цепь из 3 hop'ов
    msgs := bob.dht.GetViaCircuit(circuit, "mailbox_msg:" + bob.currentMailboxKey)

    // 2. Расшифровываем
    for _, msg := range msgs {
        plaintext := crypto.Decrypt(bob.mailboxPriv, msg.Ciphertext)
        bob.handleMessage(plaintext)

        // 3. Ротируем mailbox key
        bob.rotateMailboxKey()
    }
}
```

## Ротация ключей

После каждого полученного сообщения Боб генерирует новый mailbox key:

```go
func (bob *Node) rotateMailboxKey() {
    // 1. Генерируем новый keypair
    newPub, newPriv := box.GenerateKey(rand.Reader)

    // 2. В ответе Алисе (через bridge) передаём новый ключ:
    response := {
        "reply": "...",                         // ответное сообщение
        "next_mailbox_key": hex(newPub),        // следующий ключ для Алисы
    }
    encrypted := box.Seal(response, alicePub, bob.mailboxPriv)

    // 3. Удаляем старый ключ из DHT
    bob.dht.Delete("mailbox:" + bob.oldMailboxKey)

    // 4. Публикуем новый
    bob.dht.Put("mailbox:" + hex(newPub), &MailboxRecord{...})

    bob.currentMailboxKey = hex(newPub)
    bob.mailboxPriv = newPriv
}
```

## Двухуровневая экономика

| Уровень | Валюта | Где | Как работает |
|---|---|---|---|
| **Локальный** | RELAY credits | Внутри mesh, offline | PoR, mutual credit |
| **Глобальный** | RELAY token | Через bridge | Конвертация credits ↔ токен |

Bridge-оператор:
1. Платит реальные деньги за интернет (ISP)
2. Получает RELAY credits внутри mesh за доставку внешних сообщений
3. Продаёт доступ внешним пользователям за фиат/крипту
4. Прибыль = внешняя выручка − ISP − затраты на RELAY

## Внешний пользователь → Внутренний

```
1. Алиса (снаружи) хочет отправить сообщение Бобу
2. Алиса знает: "Боб в этой mesh-сети, ID = bob_nickname"
3. Алиса → bridge_api: GET /api/mailbox/bob_nickname
4. Bridge возвращает MailboxKey + цену доставки
5. Алиса платит bridge'у (криптой/фиатом)
6. Алиса шифрует сообщение MailboxKey'ом
7. Bridge кладёт в DHT mesh
8. Боб забирает через onion-цепь, ротирует ключ
```

## Bridge discovery

Как Алиса находит bridge?

- Bridge-узлы публикуют свои контактные данные в открытом доступе
  (это легально, bridge — легитимный сервис)
- Несколько bridge'ей → конкуренция по цене и надёжности
- Репутация bridge'а (внутри mesh) определяет доверие
