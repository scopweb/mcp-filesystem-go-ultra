# Plan — próximas mejoras (editor parity + AST Go)

> Estado: **propuesta, sin implementar**. Para discutir y elegir orden.
> Contexto: derivado de comparar el editor integrado de Cowork (state-tracking,
> "nunca releer") con filesystem-ultra. Objetivo: que filesystem-ultra pueda
> usarse como editor único sin perder su red de seguridad.

## Orden propuesto y dependencias

**AST-Go → (1+3 juntos) → 2 → 4.**

- AST-Go: el más autocontenido y de menor riesgo → primero.
- 1 y 3 están **acoplados** (hacer 1 sin 3 es a medias) → juntos.
- 2 es independiente, reutiliza `core/line_range.go`.
- 4 es el más arriesgado y **depende de 1** → último.

| Item | Esfuerzo | Riesgo | Depende de |
|---|---|---|---|
| Validador AST Go | bajo (~½ día) | bajo | — |
| 1 — content_hash post-edición | bajo | bajo | — |
| 3 — respuestas estructuradas | medio | medio (aditivo) | 1 |
| 2 — `mode:"replace_range"` | bajo-medio | bajo-medio | — |
| 4 — OCC de sesión automático | medio-alto | medio-alto | 1 |

---

## Validador AST de Go (primero)

Extiende el módulo del punto 2 (`core/structure_check.go`) con un validador
específico para `.go` usando `go/parser` + `go/token` de la stdlib. Cero
dependencias, en proceso, milisegundos.

**Diseño:**
- Nueva func `CheckGoSyntax(oldContent, newContent, path) string`: si ext `.go`,
  `parser.ParseFile(fset, path, newContent, parser.SkipObjectResolution)`; si
  falla, warning con la posición (`línea:col` del primer error).
- **Principio delta** (igual que el balance): solo avisa si el viejo parseaba y
  el nuevo no → un fragmento ya roto no genera ruido.
- Para `.go`, el AST **sustituye** al balance de llaves (parsea de verdad, no
  cuenta símbolos); el balance léxico (`CheckBalanceDelta`) sigue para
  `.cs/.razor/.js/...`. Evita doble warning.
- Gancho: en `EditFile`/`MultiEdit`/`DeleteLineRange` (y `ReplaceRange` cuando
  exista), warning-only, nunca bloquea. Reusa el campo `StructureWarning`.

**Coherencia:** no duplica `go vet` — el valor es la **inmediatez** (avisa al
editar, no en el ciclo de build, que era la queja del punto 2 original).

**Tests:** `core/structure_check_test.go` — añadir casos: edición que rompe el
parseo de un `.go` → warning; edición válida → sin warning; archivo ya roto
antes → sin warning (delta); extensión no-Go → no usa AST.

---

## Punto 1 — `content_hash` resultante en cada edición

Devolver el hash del archivo ya editado para encadenar `expected_hash` sin
releer (replica el loop "no re-read" del editor nativo).

**Diseño:**
- Añadir `NewHash string` a `EditResult` y `MultiEditResult`
  (`core/edit_operations.go`); calcularlo en el engine sobre
  `finalContent`/`remaining` (los bytes realmente escritos tras `restoreEOL`),
  sin releer de disco. Como `hash(finalContent) == hash(bytes en disco)`, casa
  con lo que validaría un `expected_hash` posterior.
- También en `DeleteLineRange`/`ReplaceLineRange` (`core/line_range.go`).
- Surface como campo estructurado `content_hash` (misma clave que `read_file`)
  → reutiliza el mecanismo existente.

**Se implementa junto al punto 3** (el `new_hash` es uno de los campos
estructurados).

**Tests:** tras un `edit_file`, el `content_hash` devuelto == FNV de los bytes
en disco == hash que aceptaría un `expected_hash` encadenado sin releer.

---

## Punto 3 — respuestas estructuradas en ops de edición

**Diseño:**
- Definir un struct `EditResponse` serializado como `structuredContent`
  **además** del texto actual (no en vez de): `replacements, lines_added,
  lines_removed, total_lines, backup_id, previous_backup_id, risk,
  structure_warning, new_hash`.
- Mantener el texto idéntico (compact y verbose) para no romper consumidores ni
  los tests que asertan formato — puramente aditivo.
- Beneficio colateral: elimina el frágil `parseReplacementCount(respText)` de
  `audit.go` (el handler ya tiene el conteo).

**Archivos:** `tools_core.go` (edit_file), `tools_batch.go` (multi_edit, batch),
`core/line_range.go` (delete_range/replace_range handlers).

**Tests:** parsear el `structuredContent` y verificar que coincide con el texto;
asegurar que el texto no cambió (regresión de formato).

---

## Punto 2 — `mode:"replace_range"`

Reemplazar líneas X–Y por contenido nuevo, por número de línea, sin match
frágil de `old_text`. Compañero natural de las lecturas por rango.

**Diseño:**
- `core/line_range.go`: nueva `ReplaceLineRange(ctx, path, start, end, newText)`
  espejo de `DeleteLineRange` — splice `lines[:start-1] + newText + lines[end:]`,
  backup, escritura atómica, devuelve result + `NewHash`.
- **Sin params nuevos**: reutiliza `start_line`/`end_line` (ya añadidos en el
  punto 4 anterior) + `new_text`. El validador ya los acepta.
- **Detalle clave (bordes):** si `new_text` no acaba en `\n` y hay líneas
  detrás, insertar el salto para no pegar líneas. Casos con/sin newline final,
  reemplazo de la última línea, rango que excede el archivo (clamp).

**Tests:** `core/line_range_test.go` — byte-exact del splice en todos los bordes.

---

## Punto 4 — OCC de sesión automático (el más delicado)

Detectar staleness aunque el cliente no pase `expected_hash`.

**Diseño:**
- Extiende el sistema existente (`RecordRead` / stale-read en
  `core/feedback.go`): guardar hash por `(sesión, path)` con TTL/eviction, usando
  `CurrentSessionID()`.
- **Trampa crítica:** actualizar el hash conocido **también al escribir/editar**
  (reusando el `NewHash` del punto 1). Si no, tras una edición la siguiente
  vería un falso mismatch contra la lectura. Por eso 4 depende de 1.
- **Default warn, no block.** Un falso positivo que bloquea es peor que el
  problema. Flag para subir a block opcionalmente.

**Tests:** lectura → modificación externa simulada → edición sin
`expected_hash` detecta staleness; lectura → edición propia → siguiente edición
NO da falso positivo (el known-hash se actualizó).

---

## Fuera de alcance (decidido)

- **LSP / language servers**: otro producto (cliente JSON-RPC, gestión de
  procesos, servidores instalados por el usuario, warmup). Rompe el "rápido, en
  proceso, sin dependencias".
- **AST multi-lenguaje (tree-sitter)**: CGO rompe el build pure-Go
  (`-trimpath -s -w`, cross-compile Windows, binario pequeño). Para C#/Razor/JS
  el **build** ya es el chequeo semántico (`dotnet_build`, etc.) — duplicarlo va
  contra la regla de no duplicar funcionalidad. Solo el AST de Go (stdlib) entra.
