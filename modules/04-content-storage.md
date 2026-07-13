# L3: Content-Addressed Storage

## Назначение

Децентрализованное хранилище файлов, где данные идентифицируются хэшем содержимого,
а не расположением. Аналог IPFS + BitTorrent, адаптированный под mesh без интернета.

## Content addressing

```
File → SHA256(file) → FileHash = 0xDEAD...BEEF
File → chunk1, chunk2, ..., chunkN (каждый 256 KB)
Chunk → SHA256(chunk) → ChunkHash

Manifest:
{
  "file_hash": "0xDEAD...BEEF",
  "total_size": 15728640,
  "chunk_size": 262144,
  "chunks": [
    "0xAAA...001",
    "0xBBB...002",
    ...
  ]
}
```

Manifest тоже хранится в DHT по ключу FileHash.

## Erasure Coding (Reed-Solomon)

Простое чанкование хрупкое: потерял любой чанк → файл битый. Erasure coding решает это:

```
K = 10 чанков данных
M = 15 чанков после кодирования (избыточность 1.5x)

Нужно ЛЮБЫХ K=10 из M=15 для восстановления.
Потеря до 5 чанков допустима.
```

Параметры:
- K = 10 исходных чанков (блоков данных)
- M = 15 закодированных чанков
- Размер чанка: 256 KB
- Файл до 2.5 MB на один erasure-блок, большие файлы — несколько блоков

## Параллельное скачивание (BitTorrent-like)

```
Клиент хочет скачать файл по FileHash

1. DHT.Get(FileHash) → Manifest {chunks: [hash1, hash2, ..., hashM]}
2. Для каждого chunk hash:
   DHT.GetProvider(hash) → [peer1, peer2, peer3]  (узлы, имеющие этот чанк)
3. Скачиваем параллельно:
   chunk1 ← peer1 (WiFi)
   chunk2 ← peer2 (LoRa)
   chunk3 ← peer3 (LoRa)
   ...
4. Как только скачано K из M чанков → декодируем через Reed-Solomon → файл готов
5. Клиент становится provider'ом для скачанных чанков (автоматический seeding)
```

## Адаптивная репликация

```go
func (n *Node) replicationCheck(manifestHash string) {
    manifest := n.dht.Get(manifestHash)

    for _, chunkHash := range manifest.Chunks {
        providers := n.dht.GetProviders(chunkHash)
        if len(providers) < REPLICATION_FACTOR {
            // Чанк под угрозой исчезновения — запрашиваем и сохраняем
            data := n.fetchChunk(chunkHash)
            n.storeChunk(chunkHash, data)
            n.dht.AnnounceProvider(chunkHash, n.peerID)
        }
    }
}
```

- REPLICATION_FACTOR = 3 (минимум 3 копии каждого чанка в сети)
- Проверка каждые 30 минут
- Если узел уходит офлайн — другие узлы автоматически подхватывают replication

## Proof of Content (PoC)

Аналог Filecoin's Proof of Replication / Proof of Spacetime, но упрощённый:

```go
// Узел периодически доказывает, что хранит чанк
func (n *Node) generateChunkProof(chunkHash string) *Proof {
    chunk := n.loadChunk(chunkHash)
    challenge := randomBytes(32)  // от соседа

    // Доказательство = хэш(chunk + challenge)
    proof := sha256(chunk + challenge)

    return &Proof{
        ChunkHash: chunkHash,
        Challenge: challenge,
        Response:  proof,
    }
}
```

- Сосед периодически отправляет challenge
- Узел должен ответить proof'ом (невозможно без хранения чанка)
- Proof валидируется соседом: `sha256(ожидаемый_чанк + challenge) == proof.Response`
- Успешные proof'ы повышают reputation за хранение
- Хранение оплачивается: STORAGE_REWARD = 0.01 credit/MB/час (см. `05-tokenomics.md`)

## Зеркалирование сервисов через контентный слой

Сервис (сайт) публикует свои статические файлы в контентный слой:

```
students-wiki/
  index.html   → ChunkHash = 0xAAA
  style.css    → ChunkHash = 0xBBB
  app.js       → ChunkHash = 0xCCC
  data.json    → ChunkHash = 0xDDD

ServiceRecord в DHT:
{
  "name": "students-wiki",
  "owner": "0xABCD",
  "files": {
    "/index.html": "0xAAA",
    "/style.css":  "0xBBB",
    "/app.js":     "0xCCC",
    "/data.json":  "0xDDD"
  }
}
```

Теперь **любой узел** с достаточным дисковым пространством может стать
mirror'ом сервиса, просто скачав и храня эти чанки. Отказоустойчивость
сервиса = отказоустойчивость контентной сети.
