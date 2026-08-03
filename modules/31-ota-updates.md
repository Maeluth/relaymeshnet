# OTA Updates & Safe Firmware Deployment 🟡 Required

> **Required.** Без OTA-обновлений через mesh каждый узел нужно обновлять вручную (USB/физический доступ). Для 500+ узлов это невозможно. Обновления должны быть безопасными, подписанными и с возможностью отката.

## Принцип

```
1. Разработчик → подписывает прошивку Ed25519
2. Прошивка → чанки в content-addressed storage
3. Узлы → периодически проверяют DHT на наличие новой версии
4. Если есть → скачивают через BitTorrent-подобный механизм
5. Проверяют подпись → устанавливают → перезагружаются
6. При ошибке → автоматический откат к предыдущей версии
```

## Формат прошивки

```go
type FirmwarePackage struct {
    Version     int       // версия формата пакета
    FirmwareVer string    // "1.2.3"
    MinHW       HardwareProfile  // минимальные требования к железу
    Timestamp   uint64    // когда собрана
    Chunks      []FirmwareChunk
    Signature   []byte    // Ed25519 подпись разработчика
    DevPubKey   [32]byte  // публичный ключ разработчика
}

type FirmwareChunk struct {
    Index    int       // 0, 1, 2...
    Hash     [32]byte  // SHA256(chunk_data)
    Size     int       // размер в байтах
    Offset   int64     // смещение в итоговом бинарнике
}
```

## Подпись прошивки

```go
// Разработчик при сборке:
func SignFirmware(firmware []byte, devPriv ed25519.PrivateKey) (*FirmwarePackage, error) {
    // 1. Разбиваем на чанки по 256 KB
    chunkSize := 256 * 1024
    var chunks []FirmwareChunk
    for i := 0; i < len(firmware); i += chunkSize {
        end := i + chunkSize
        if end > len(firmware) { end = len(firmware) }
        data := firmware[i:end]
        chunks = append(chunks, FirmwareChunk{
            Index:  len(chunks),
            Hash:   sha256.Sum256(data),
            Size:   len(data),
            Offset: int64(i),
        })
    }
    
    // 2. Собираем пакет
    pkg := &FirmwarePackage{
        Version:     1,
        FirmwareVer: version,
        Timestamp:   uint64(time.Now().Unix()),
        Chunks:      chunks,
        DevPubKey:   devPubKey,
    }
    
    // 3. Подписываем
    hash := sha256.Sum256(pkg.Bytes())  // сериализуем без Signature
    sig := ed25519.Sign(devPriv, hash[:])
    pkg.Signature = sig
    
    return pkg, nil
}
```

## Процесс установки

### Phase 1: Проверка

```go
func (n *Node) CheckForUpdate() (*FirmwarePackage, error) {
    // 1. Получаем текущую версию
    currentVer := n.config.Version
    
    // 2. Проверяем DHT: есть ли более новая версия?
    // Ключ: "firmware:maeluth/rmn-core/latest"
    record := n.dht.Get("firmware:maeluth/rmn-core/latest")
    if record == nil {
        return nil, nil  // нет обновлений
    }
    
    pkg := parsePackage(record.Value)
    
    // 3. Новее ли версия?
    if !isNewer(pkg.FirmwareVer, currentVer) {
        return nil, nil
    }
    
    // 4. Подходит ли под железо?
    if !n.hardware.MeetsMinimum(pkg.MinHW) {
        n.log.Warn("Update requires better hardware, skipping")
        return nil, nil
    }
    
    return pkg, nil
}
```

### Phase 2: Скачивание

```go
func (n *Node) DownloadFirmware(pkg *FirmwarePackage) ([]byte, error) {
    firmware := make([]byte, totalSize(pkg.Chunks))
    
    // Скачиваем чанки параллельно (как BitTorrent)
    var wg sync.WaitGroup
    errCh := make(chan error, len(pkg.Chunks))
    
    for _, chunk := range pkg.Chunks {
        wg.Add(1)
        go func(c FirmwareChunk) {
            defer wg.Done()
            data, err := n.contentStore.Fetch(c.Hash)
            if err != nil {
                errCh <- err
                return
            }
            copy(firmware[c.Offset:], data)
        }(chunk)
    }
    
    wg.Wait()
    close(errCh)
    
    if err := <-errCh; err != nil {
        return nil, err
    }
    
    return firmware, nil
}
```

### Phase 3: Верификация

```go
func VerifyFirmware(pkg *FirmwarePackage, firmware []byte) error {
    // 1. Проверяем подпись разработчика
    hash := sha256.Sum256(pkg.Bytes())
    if !ed25519.Verify(pkg.DevPubKey, hash[:], pkg.Signature) {
        return ErrInvalidSignature
    }
    
    // 2. Проверяем чанки
    for _, chunk := range pkg.Chunks {
        data := firmware[chunk.Offset : chunk.Offset+int64(chunk.Size)]
        actualHash := sha256.Sum256(data)
        if actualHash != chunk.Hash {
            return fmt.Errorf("chunk %d: hash mismatch", chunk.Index)
        }
    }
    
    // 3. Проверяем что прошивка НЕ повреждена
    // (дополнительные проверки: ELF header, magic bytes, etc.)
    
    return nil
}
```

### Phase 4: Установка

```go
func (n *Node) InstallFirmware(firmware []byte, pkg *FirmwarePackage) error {
    // 1. Сохраняем новую прошивку в отдельный раздел
    //    (не перезаписываем текущую!)
    newPath := "/etc/rmn/firmware/firmware_new.bin"
    
    if err := os.WriteFile(newPath, firmware, 0755); err != nil {
        return err
    }
    
    // 2. Сохраняем метаданные
    meta := UpdateMetadata{
        PreviousVer: n.config.Version,
        NewVer:      pkg.FirmwareVer,
        InstalledAt: time.Now().Unix(),
        Package:     pkg,
    }
    saveMetadata("/etc/rmn/firmware/update_meta.json", meta)
    
    // 3. Устанавливаем флаг "обновление готово"
    //    При следующей перезагрузке загрузится новая прошивка
    os.WriteFile("/etc/rmn/firmware/UPDATE_READY", []byte("1"), 0644)
    
    // 4. Плавная перезагрузка
    n.log.Info("Firmware ready, restarting...")
    go func() {
        time.Sleep(3 * time.Second)  // даём время на gossip "я обновляюсь"
        n.shutdown()
        exec.Command("reboot").Run()
    }()
    
    return nil
}
```

### Phase 5: Откат при ошибке

```go
// При старте проверяем был ли откат
func (n *Node) CheckRollback() bool {
    // Был ли флаг UPDATE_READY при ПРОШЛОМ запуске?
    if _, err := os.Stat("/etc/rmn/firmware/UPDATE_READY"); err == nil {
        // Удаляем флаг — обновление применено успешно
        os.Remove("/etc/rmn/firmware/UPDATE_READY")
        
        // Проверяем что прошивка работает (прошли bootstrap)
        if !n.bootstrapSuccessful() {
            n.rollback()
            return true
        }
    }
    
    // Проверяем счётчик откатов
    meta := loadMetadata("/etc/rmn/firmware/update_meta.json")
    if meta.RollbackCount > 3 {
        n.log.Error("Too many rollbacks, staying on current version")
        return false
    }
    
    return false
}

func (n *Node) rollback() {
    meta := loadMetadata("/etc/rmn/firmware/update_meta.json")
    meta.RollbackCount++
    meta.RollbackAt = time.Now().Unix()
    saveMetadata("/etc/rmn/firmware/update_meta.json", meta)
    
    // Восстанавливаем предыдущую версию
    oldPath := "/etc/rmn/firmware/firmware_old.bin"
    newPath := "/etc/rmn/firmware/firmware_new.bin"
    
    os.Rename(newPath, "/etc/rmn/firmware/firmware_failed.bin")
    os.Rename(oldPath, newPath)
    
    n.log.Warn("Rolling back to previous firmware version")
    exec.Command("reboot").Run()
}
```

## Частичное обновление

Не всегда нужно обновлять ВСЮ прошивку. Можно обновить только один модуль:

```go
type PartialUpdate struct {
    Module   string  // "onion", "dht", "transport", "webui"
    Version  string
    Chunks   []FirmwareChunk  // только чанки этого модуля
    Deps     []string  // зависимости (другие модули нужной версии)
}

func (n *Node) ApplyPartialUpdate(update PartialUpdate) error {
    // 1. Проверяем зависимости
    for _, dep := range update.Deps {
        if !n.hasModule(dep) {
            return fmt.Errorf("missing dependency: %s", dep)
        }
    }
    
    // 2. Скачиваем и верифицируем чанки модуля
    data, err := n.downloadModule(update.Module, update.Chunks)
    if err != nil { return err }
    
    // 3. Заменяем модуль
    modulePath := fmt.Sprintf("/etc/rmn/modules/%s.so", update.Module)
    os.WriteFile(modulePath, data, 0755)
    
    // 4. Перезагружаем только этот модуль (hot-reload)
    n.reloadModule(update.Module)
    
    return nil
}
```

## Gossip-распространение информации об обновлении

```go
// Узел, успешно обновившийся, рассылает gossip:
type UpdateGossip struct {
    NodeID     [16]byte
    OldVersion string
    NewVersion string
    InstalledAt uint64
    Success    bool
    Signature  []byte  // подпись узла
}

// Другие узлы видят gossip и решают обновляться
func (n *Node) OnUpdateGossip(gossip UpdateGossip) {
    // Проверяем что N узлов уже обновились успешно
    successCount := n.countSuccessfulUpdates(gossip.NewVersion)
    
    if successCount >= n.config.MinUpdateConfirmations {
        n.log.Info("Sufficient nodes updated, starting update")
        n.triggerUpdate(gossip.NewVersion)
    }
}
```

## Параметры

| Параметр | Значение | Описание |
|---|---|---|
| `UPDATE_CHECK_INTERVAL` | 6 часов | Как часто проверять обновления |
| `UPDATE_CHUNK_SIZE` | 256 KB | Размер чанка прошивки |
| `MAX_ROLLBACK_COUNT` | 3 | Максимум попыток отката |
| `MIN_UPDATE_CONFIRMATIONS` | 10 | Минимум узлов с успешным обновлением |
| `UPDATE_TIMEOUT` | 1 час | Таймаут скачивания прошивки |
| `PARTIAL_UPDATE_ENABLED` | true | Разрешить частичные обновления |
