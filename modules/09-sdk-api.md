# SDK & Development 🟢 Optional

> **Optional.** SDK — удобная обёртка над Core API. Разработчик может писать напрямую через wire format. SDK ускоряет разработку, но не обязателен для работы сети.

## Назначение

RelayMeshNet SDK — Go-пакет, позволяющий разработчикам интегрироваться
с mesh-сетью и создавать децентрализованные приложения поверх неё.

## Подключение к сети

```go
package main

import (
    "fmt"
    mdl "github.com/RelayMeshNet/v3/pkg/sdk"
)

func main() {
    // Автоматическое подключение: ищет ближайший роутер, подключается
    node, err := mdl.Connect(mdl.Config{
        Mode: mdl.ModeClient,  // или ModeHost, ModeBridge
    })
    if err != nil {
        panic(err)
    }
    defer node.Close()

    fmt.Printf("PeerID: %s\n", node.PeerID())
    fmt.Printf("Balance: %d RELAY\n", node.Balance())
    fmt.Printf("Reputation: %.2f\n", node.Reputation())
}
```

## Регистрация сервиса

```go
// Поднимаем HTTP-сервис внутри mesh
func main() {
    node, _ := mdl.Connect(mdl.Config{Mode: mdl.ModeHost})

    mux := http.NewServeMux()
    mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
        w.Write([]byte("<h1>My Wiki</h1>"))
    })

    // Регистрируем в DHT — сайт виден всем в сети
    serviceID, err := node.RegisterService("my-wiki", mux, mdl.ServiceConfig{
        Public:    true,   // виден в каталоге
        Replicas:  3,       // зеркалировать на 3 узла
    })

    fmt.Printf("Service: http://%s\n", serviceID)
    select {} // держим сервер
}
```

## Отправка сообщения

```go
// E2E зашифрованное сообщение через onion
err := node.SendMessage(recipientPeerID, []byte("Hello!"), mdl.MessageConfig{
    Hops:      3,          // onion hops (минимум 1, см. 13-hybrid-economy.md)
    Encrypted: true,       // E2E NaCl box
    Priority:  mdl.PriorityNormal,
})

// Broadcast сообщения (всем в сети)
err := node.Broadcast([]byte("Network announcement"), mdl.BroadcastConfig{
    TTL: 24 * time.Hour,
})
```

## Контентное хранилище

```go
// Сохранить файл в контентную сеть
contentHash, err := node.StoreContent([]byte("file data..."), mdl.ContentConfig{
    Replicas:    5,       // минимум 5 копий
    ErasureK:    10,      // Reed-Solomon K
    ErasureM:    15,      // Reed-Solomon M
    Encrypt:     true,    // зашифровать перед хранением
})

// Получить файл из контентной сети
data, err := node.FetchContent(contentHash)

// Потоковая загрузка с прогрессом
stream, _ := node.FetchContentStream(contentHash)
for progress := range stream.Progress {
    fmt.Printf("Downloading: %.1f%%\n", progress.Percent)
}
data := <-stream.Data
```

## Подписка на события

```go
// Подписка на новые сервисы в сети
sub := node.Subscribe(mdl.TopicNewServices)
for event := range sub.Events {
    fmt.Printf("New service: %s by %s\n", event.ServiceName, event.Owner)
}

// Подписка на сообщения (чат)
msgSub := node.Subscribe(mdl.TopicMessages)
for msg := range msgSub.Events {
    fmt.Printf("[%s] %s: %s\n", msg.Channel, msg.Sender, msg.Text)
}

// Подписка на peer events
peerSub := node.Subscribe(mdl.TopicPeers)
for event := range peerSub.Events {
    fmt.Printf("Peer %s is now %s\n", event.PeerID, event.Status)
}
```

## DHT-запросы

```go
// Поиск сервисов
services, _ := node.DHT().SearchServices("wiki")
for _, svc := range services {
    fmt.Printf("%s (reputation: %.1f, online: %d mirrors)\n",
        svc.Name, svc.OwnerReputation, svc.MirrorCount)
}

// Поиск пиров
peers, _ := node.DHT().QueryPeers(mdl.PeerFilter{
    MinReputation: 10.0,
    MinUptime:     24 * time.Hour,
    Limit:         50,
})

// Произвольный key-value в DHT
node.DHT().Put("myapp:config", []byte(`{"theme":"dark"}`), mdl.DHTOpts{
    TTL: 1 * time.Hour,
})
value, _ := node.DHT().Get("myapp:config")
```

## Управление токенами

```go
// Проверить баланс
balance := node.Balance()
creditLimit := node.CreditLimit()

// Отправить RELAY другому пользователю
txHash, err := node.Transfer(recipientPeerID, 100, mdl.TransferConfig{
    Hops: 3,  // анонимный перевод через onion
})

// Разместить ордер на бирже
orderID, _ := node.Exchange().PlaceOrder(mdl.Order{
    Type:  mdl.OrderSell,
    Amount: 1000,
    Price:  0.002,  // RELAY/KB
})

// Получить ордербук
book, _ := node.Exchange().OrderBook()
```

## Администрирование узла

```go
// Статистика узла
stats := node.Stats()
// {
//   uptime_hours: 72.5,
//   relay_in_bytes: 10485760,
//   relay_out_bytes: 8388608,
//   reputation: 15.3,
//   peers_connected: 42,
//   dht_entries: 1503,
//   content_stored_bytes: 52428800,
//   balance: 250
// }

// Настройка relay-политики
node.SetRelayPolicy(mdl.RelayPolicy{
    MinReputation:  5.0,     // не релеить узлам с репутацией ниже
    MaxHops:        5,       // максимум hop'ов в relay-цепи
    BandwidthLimit: 102400,  // байт/сек на relay
    Whitelist:      []string{"peer1", "peer2"},  // всегда релеить этим
    Blacklist:      []string{"peer3"},            // никогда не релеить этим
})

// Экспорт/импорт ключей
backup := node.ExportIdentity("passphrase")
node.ImportIdentity(backup, "passphrase")
```

## WebUI API

Встроенный WebUI доступен по `http://localhost:8080` (в режиме Client).
Разработчик может встроить свой фронтенд:

```go
// Добавить свою страницу в WebUI
node.WebUI().AddRoute("/myapp", myAppHandler)

// Или полностью заменить фронтенд
node.WebUI().SetHandler(myCustomUI)
```
