# Mejoras 1–6 — Implementación

Implementadas en el orden acordado: **3 → 6b → 1 → 6a → 2 → 5 → 4**.

> ⚠️ **Verificación pendiente en tu máquina.** El entorno donde se editó no tiene
> Go ni acceso de red para instalarlo, así que **no se ha podido compilar ni
> correr los tests aquí**. Hazlo tú en Windows (Go 1.26.3) — ver sección Build.

---

## Resumen por punto

### Punto 3 — `content_hash` en lecturas parciales
`read_file` ahora devuelve el `content_hash` (FNV-1a de los bytes crudos, el mismo
token OCC que valida `edit_file`/`multi_edit` vía `expected_hash`) también en modo
**rango**, **head/tail** y **base64**, no solo en lectura completa. El servidor lee
el archivo entero para hashear pero solo devuelve el fragmento pedido: el coste de
tokens queda acotado al rango.
- `tools_core.go`: helper `computeFileOCCHash`; ramas base64 y rango.
- Descripción de `expected_hash` actualizada.

### Punto 6b — auditoría `in_progress`
`auditWrap` escribe una entrada `status:"in_progress"` con `req_id` **antes** de
ejecutar el handler, y la entrada final comparte el mismo `req_id`. Una llamada
interrumpida (corte de transporte al cambiar de superficie) deja una línea
`in_progress` huérfana en `operations.jsonl` → diagnóstico de en qué fase cayó.
Guardado tras `AuditEnabled()` (cero coste sin `--log-dir`).
- `audit.go`: `newRequestID`, breadcrumb pre-handler.
- `core/audit_logger.go`: campo `RequestID` (`req_id`).

### Punto 1 — `diff_format` en dry_run
Nuevo parámetro `diff_format` en `edit_file`: `""`/`auto` (default: completo si es
pequeño, summary + hint si es grande), `full`, `summary` (rangos + 3 líneas de
ancla, elide el cuerpo), `stat` (`+N -M`), `none`. Unifica el comportamiento entre
modos replace/regex/search_replace (antes regex volcaba el diff completo siempre).
- `core/diff.go`: `RenderDiff`, `formatHunksSummary`, refactor a `formatHunksFull`.
- `tools_core.go`: helper `diffFormatArg`; 3 puntos de emisión usan `RenderDiff`.

### Punto 6a — escritura atómica en `batch_operations`
`executeWrite` usaba `os.WriteFile` directo (no atómico). Ahora usa el helper
compartido `atomicWriteFile` (temp + rename), igual que `write_file`. Un batch
cortado a mitad nunca deja archivo parcial.
- `core/engine.go`: helper `atomicWriteFile` (consolida el patrón duplicado).
- `core/batch_operations.go`: `executeWrite` lo usa, preservando el modo del archivo.

### Punto 2 — verificación estructural post-edición (delta de balance)
Tras editar archivos de código, se compara el balance de `{} () []` de old vs new.
Si **estaba balanceado antes y no después**, la edición introdujo el desbalanceo →
warning (nunca bloquea; se adjunta a la respuesta y al audit). El enfoque *delta*
evita falsos positivos en fragmentos o archivos ya desbalanceados. Ignora llaves
dentro de strings y comentarios (scanner C-like).
- `core/structure_check.go`: `delimiterBalance`, `CheckBalanceDelta`, `isBalanceCheckedExt`.
- `core/edit_operations.go`: campo `StructureWarning` en `EditResult` y `MultiEditResult`; check en `EditFile` y `MultiEdit`.
- `tools_core.go` / `tools_batch.go`: surface del warning (compact + verbose).

### Punto 5 — desacoplar `force` del rewrite guard
`force` ya **no** bypassea el guard de rewrite accidental. Se añade un flag
dedicado `allow_rewrite` para eso. `force` queda reservado al umbral de riesgo.
El mensaje del guard recomienda `write_file` y aclara que `force` no lo salta.
(Recordatorio del análisis: por el AND de 3 señales, el guard casi nunca dispara
en rewrites legítimos, así que el riesgo de normalizar el flag ya era bajo.)
- `tools_core.go`: parse de `allow_rewrite`; guard usa `!allowRewrite`.
- `core/feedback.go`: doc y sugerencia actualizadas.
- `core/param_validator.go`: `allow_rewrite`.

### Punto 4 — `delete_range` + acción `extract`
- **`edit_file` mode `delete_range`**: elimina líneas `[start_line, end_line]`
  (atómico, con backup). Primitiva nueva útil por sí sola.
- **`batch_operations` acción `extract`**: mueve líneas de `source` a `destination`
  usando **los mismos bytes** para escribir y borrar (garantiza escrito == borrado),
  con backup de ambos archivos y rollback conjunto bajo `atomic:true`.
- `core/line_range.go`: `ComputeLineRangeDeletion` (byte-exact), `DeleteLineRange`.
- `core/batch_operations.go`: tipo `extract` (validate/dispatch/rollback/backup), `executeExtract`; campos `StartLine`/`EndLine`/`Append` en `FileOperation`.

---

## Build & Test (en Windows)

```bash
# Compilar el binario v4
go build -ldflags="-s -w" -trimpath -o filesystem-ultra-v4.exe .

# Vet (rápido, detecta errores de tipos/imports)
go vet ./...

# Formato (los edits via tool pueden no estar gofmt-clean)
gofmt -w core/ *.go

# Tests
go test ./core/... ./...

# Tests nuevos en concreto
go test ./core/ -run "TestComputeLineRangeDeletion|TestCheckBalanceDelta|TestDelimiterBalance|TestRenderDiff" -v
go test . -run "TestComputeFileOCCHash" -v
```

Tests añadidos:
- `core/line_range_test.go` — garantía byte-exact de extract + errores.
- `core/structure_check_test.go` — balance delta y exclusión de strings/comentarios.
- `core/diff_render_test.go` — formatos de diff y auto-collapse.
- `occ_hash_partial_read_test.go` — el hash de lectura parcial == FNV de bytes crudos.

---

## Follow-ups / decisiones abiertas

1. **Dashboard** (`cmd/dashboard/`): no se tocó. Ahora `operations.jsonl` tiene 2
   líneas por op (`in_progress` + final). Conviene que la página Operations agrupe
   por `req_id` y marque las `in_progress` huérfanas como "interrumpidas". Las
   métricas (`metrics.json`) no se ven afectadas (vienen de contadores, no del log).
2. **`gofmt`**: varios edits se hicieron con indentación que puede no estar
   gofmt-clean. Pasar `gofmt -w` antes de commitear.
3. **Punto 6c (transporte stdio→HTTP)**: sigue siendo decisión de configuración del
   conector, fuera de este código.
4. **`verify_structure` como parámetro**: el punto 2 quedó auto por extensión de
   código (cubre el caso .razor/.cs que sufriste). Si quieres poder forzarlo en
   extensiones desconocidas o silenciarlo, habría que propagar un flag por la firma
   de `EditFile`/`MultiEdit` — lo dejé fuera para no tocar todas las llamadas.
