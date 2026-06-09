# Plan: singleflight + ReadFileRange cache (PR read-dedup)

**Estado:** Completado (fase G — 2026-06-09)  
**Objetivo:** Deduplicar lecturas concurrentes en cache miss y reutilizar BigCache en `ReadFileRange`.

## Checkpoint — dónde parar y retomar

| Fase | Archivos | Hecho cuando… |
|------|----------|---------------|
| **A** | `docs/plans/READ_DEDUP_PLAN.md` | Plan escrito ✅ |
| **B** | `core/read_dedup.go` | `readFileBytesDeduped` + `extractLineRange` + `readFlight` ✅ |
| **C** | `core/engine.go` | `ReadFileContent` usa dedup; `InvalidateCache` hace `Forget` ✅ |
| **D** | `core/file_operations.go` | `ReadFileRange` cache-hit + dedup si ≤5MB ✅ |
| **E** | `core/read_dedup_test.go` | Test concurrencia (1 disk read) + range desde caché ✅ |
| **F** | `go.mod` | `golang.org/x/sync` direct require ✅ |
| **G** | Tests verdes | `go test ./core/... ./tests/...` ✅ |

**Si hay que parar:** anotar la última fase completada en este archivo (columna Estado) y en el PR/commit message.

## Diseño

### 1. `singleflight.Group` por path

```
ReadFileContent / ReadFileRange (miss)
  → cache.GetFile (fast path)
  → readFlight.Do(path, func() {
        cache.GetFile again (double-check)
        os.ReadFile(path)
        cache.SetFile
        return bytes
     })
```

- `InvalidateCache` / edits: `cache.InvalidateFile` + `readFlight.Forget(path)`
- Context: el líder comprueba `ctx.Err()` antes del I/O; waiters heredan resultado (lecturas cortas).

### 2. ReadFileRange

| Condición | Comportamiento |
|-----------|----------------|
| Cache hit | `extractLineRangeFromBytes` (sin disco) |
| Miss, size ≤ `LargeFileThreshold` (5MB) | `readFileBytesDeduped` → extract |
| Miss, size > 5MB | Scan con `bufio.Scanner` (comportamiento actual) |

### 3. Fuera de scope

- mmap / MmapCache
- Cambios en tools MCP / skill docs
- `get_file_info` Windows locks

## Criterios de aceptación

- [x] 10 goroutines `ReadFileContent` cold → 1 `os.ReadFile` (test con contador)
- [x] `ReadFileRange` tras warm cache no incrementa contador de disco
- [x] `InvalidateCache` + re-read devuelve contenido nuevo
- [x] Tests existentes sin regresiones

## Notas de implementación

- **Conteo de líneas:** `extractLineRangeFromBytes` usa `bytesLineScanner` que replica `bufio.Scanner` (sin línea vacía extra por `\n` final). Regresión fijada en `TestBytesLineScanner_MatchesBufioTrailingNewline`.
- **Invalidación:** `invalidateFileReadCache` reemplaza llamadas directas a `cache.InvalidateFile` en edits/moves/streaming.

## Rollback

Revertir commits en `core/read_dedup.go`, cambios en `engine.go` y `file_operations.go`.