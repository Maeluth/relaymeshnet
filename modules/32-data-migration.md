# Data Migration Between Versions 🟡 Required

> **Required.** Без спецификации миграции данных обновление прошивки может сломать БД, потерять балансы или разрушить DHT. Новая версия должна уметь читать старые данные и конвертировать их.

## Принцип

Каждая версия прошивки имеет номер версии схемы данных. При старте прошивка проверяет версию хранимых данных и применяет цепочку миграций.

```
Версия данных:  1 ──→ 2 ──→ 3 ──→ 4 (текущая)
                │     │     │
            migrate  migrate migrate
            1→2     2→3    3→4
```

## Версионирование данных

```go
// В начале файла БД хранится версия схемы
type DataVersion struct {
    SchemaVersion int    // 1, 2, 3...
    AppVersion    string // "1.2.3"
    MigratedAt    uint64 // Unix timestamp
}

// Каждая миграция — функция
type Migration struct {
    FromVersion int
    ToVersion   int
    Description string
    Apply       func(db *sql.DB) error
}
```

## Примеры миграций

### Миграция 1→2: Добавление поля reputation_demurrage

```go
var migration1to2 = Migration{
    FromVersion: 1,
    ToVersion:   2,
    Description: "Add reputation_demurrage field to nodes table",
    Apply: func(db *sql.DB) error {
        // Добавляем колонку с дефолтным значением
        _, err := db.Exec(`
            ALTER TABLE nodes 
            ADD COLUMN reputation_demurrage REAL DEFAULT 0.0
        `)
        return err
    },
}
```

### Миграция 2→3: Новая таблица для PreKeyBundle

```go
var migration2to3 = Migration{
    FromVersion: 2,
    ToVersion:   3,
    Description: "Create prekey_bundles table for X3DH support",
    Apply: func(db *sql.DB) error {
        _, err := db.Exec(`
            CREATE TABLE IF NOT EXISTS prekey_bundles (
                peer_id     TEXT PRIMARY KEY,
                identity_key BLOB NOT NULL,
                signed_prekey BLOB NOT NULL,
                signed_prekey_sig BLOB NOT NULL,
                one_time_keys BLOB NOT NULL,  -- JSON array
                created_at  INTEGER NOT NULL,
                updated_at  INTEGER NOT NULL
            )
        `)
        return err
    },
}
```

### Миграция 3→4: Конвертация баланса в целые числа

```go
var migration3to4 = Migration{
    FromVersion: 3,
    ToVersion:   4,
    Description: "Convert balances from float64 to int64 (multiply by 100)",
    Apply: func(db *sql.DB) error {
        // 1. Создаём новую таблицу
        db.Exec(`CREATE TABLE nodes_new (
            peer_id TEXT PRIMARY KEY,
            balance INTEGER NOT NULL DEFAULT 0,  -- int64, не float64!
            locked_out INTEGER NOT NULL DEFAULT 0,
            reputation REAL NOT NULL DEFAULT 0.0
        )`)
        
        // 2. Копируем данные с конвертацией
        rows, _ := db.Query(`SELECT peer_id, balance, locked_out, reputation FROM nodes`)
        for rows.Next() {
            var peerID string
            var balance, lockedOut, rep float64
            rows.Scan(&peerID, &balance, &lockedOut, &rep)
            
            db.Exec(`INSERT INTO nodes_new VALUES (?, ?, ?, ?)`,
                peerID,
                int64(balance * 100),    // float → int
                int64(lockedOut * 100),  // float → int
                rep,
            )
        }
        
        // 3. Заменяем старую таблицу
        db.Exec("DROP TABLE nodes")
        db.Exec("ALTER TABLE nodes_new RENAME TO nodes")
        
        return nil
    },
}
```

## Миграция DHT-данных

DHT-записи хранятся не в БД, а распределённо. Но локальный кэш тоже нужно мигрировать:

```go
func (n *Node) MigrateDHTCache(fromVer, toVer int) error {
    cacheFile := "/etc/rmn/cache/dht_cache.db"
    
    // Читаем старый кэш
    oldRecords, err := readCacheV1(cacheFile)
    if err != nil { return err }
    
    // Конвертируем в новый формат
    newRecords := make([]DHTRecord, len(oldRecords))
    for i, old := range oldRecords {
        newRecords[i] = old.Migrate(fromVer, toVer)
    }
    
    // Сохраняем в новом формате
    return writeCacheV2(cacheFile, newRecords)
}
```

## Миграция конфигурации

```go
func (n *Node) MigrateConfig(fromVer, toVer int) error {
    configPath := "/etc/rmn/config.json"
    data, _ := os.ReadFile(configPath)
    
    var oldConfig map[string]interface{}
    json.Unmarshal(data, &oldConfig)
    
    // Конвертируем старые ключи в новые
    newConfig := migrateConfigKeys(oldConfig, fromVer, toVer)
    
    // Сохраняем
    newData, _ := json.MarshalIndent(newConfig, "", "  ")
    os.WriteFile(configPath, newData, 0644)
    
    return nil
}

func migrateConfigKeys(old map[string]interface{}, from, to int) map[string]interface{} {
    // Пример: в v2 поле "demurrage_rate" переименовано в "rep_demurrage"
    if from <= 1 && to >= 2 {
        if val, ok := old["demurrage_rate"]; ok {
            old["rep_demurrage"] = val
            delete(old, "demurrage_rate")
        }
    }
    
    // Пример: в v3 добавлено поле "cover_traffic_rate" с дефолтом
    if from <= 2 && to >= 3 {
        if _, ok := old["cover_traffic_rate"]; !ok {
            old["cover_traffic_rate"] = 1.0  // дефолт
        }
    }
    
    return old
}
```

## Автоматическая миграция при старте

```go
func (n *Node) AutoMigrate() error {
    currentVer := n.getCurrentDataVersion()   // из БД
    targetVer := n.config.DataSchemaVersion   // из кода прошивки
    
    if currentVer == targetVer {
        return nil  // миграция не нужна
    }
    
    if currentVer > targetVer {
        return fmt.Errorf("downgrade not supported: data v%d > code v%d", 
            currentVer, targetVer)
    }
    
    // Применяем цепочку миграций
    for v := currentVer; v < targetVer; v++ {
        migration := n.migrations[v]  // миграция v → v+1
        n.log.Info("Migrating data: %d → %d (%s)", v, v+1, migration.Description)
        
        if err := migration.Apply(n.db); err != nil {
            n.log.Error("Migration %d→%d failed: %v", v, v+1, err)
            return fmt.Errorf("migration v%d→v%d failed: %w", v, v+1, err)
        }
        
        // Обновляем версию в БД
        n.setDataVersion(v + 1)
    }
    
    // Мигрируем DHT-кэш и конфигурацию
    n.MigrateDHTCache(currentVer, targetVer)
    n.MigrateConfig(currentVer, targetVer)
    
    n.log.Info("Data migration complete: v%d → v%d", currentVer, targetVer)
    return nil
}
```

## Обратная совместимость при чтении

Новая версия должна уметь читать старые данные **без миграции**:

```go
type NodeRecord struct {
    PeerID     string
    Balance    int64
    Reputation float64
}

func (n *Node) LoadNodeRecord(peerID string) (*NodeRecord, error) {
    // Пробуем новую схему
    row := n.db.QueryRow("SELECT peer_id, balance, reputation FROM nodes WHERE peer_id=?", peerID)
    var rec NodeRecord
    err := row.Scan(&rec.PeerID, &rec.Balance, &rec.Reputation)
    if err == nil {
        return &rec, nil
    }
    
    // Fallback: старая схема (balance был float64)
    row = n.db.QueryRow("SELECT peer_id, CAST(balance AS INTEGER) as balance, reputation FROM nodes_old WHERE peer_id=?", peerID)
    err = row.Scan(&rec.PeerID, &rec.Balance, &rec.Reputation)
    if err != nil {
        return nil, err
    }
    
    return &rec, nil
}
```

## Параметры

| Параметр | Значение | Описание |
|---|---|---|
| `DATA_SCHEMA_VERSION` | 1 | Текущая версия схемы данных |
| `MIGRATION_TIMEOUT` | 5 минут | Таймаут на одну миграцию |
| `BACKUP_BEFORE_MIGRATE` | true | Бэкапить данные перед миграцией |
| `MIGRATION_DRY_RUN` | false | Проверить миграцию без применения |
| `DOWNGRADE_SUPPORTED` | false | Поддерживается ли понижение версии |
