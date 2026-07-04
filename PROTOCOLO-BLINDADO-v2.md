# PROTOCOLO BLINDADO v2 — 0 RIESGO DE DESTRUCCIÓN DE ARCHIVOS

> filesystem-ultra v4.5.x · actualizado 2026-06-16
> Cambios v2: (1) regla anti-rewrite, (2) concurrencia por hash, (3) dry-run condicional.

```
┌─────────────────────────────────────────────────────────────┐
│  PROTOCOLO BLINDADO v2: 0 RIESGO DE DESTRUCCIÓN DE ARCHIVOS  │
└─────────────────────────────────────────────────────────────┘
```

---

## REGLA 0 — Anti-rewrite (NUEVA · la más importante)

El bug del 2026-06-11: `edit_file` con `old_text` pequeño + `new_text` gigante
NO reescribe el archivo, solo intercambia el fragmento → el resto queda
concatenado debajo → archivo duplicado/corrupto.

```
ANTES de cada edit_file, calcular ratio = len(new_text) / len(old_text)

├─ ratio > 2  Y  reescribes la mayoría del archivo  → write_file  (NUNCA edit_file)
├─ cambio puntual (ratio ≈ 1, mismo rango)          → edit_file mode replace
├─ reemplazo de un patrón (todas las ocurrencias)   → edit_file mode search_replace
├─ renombrar tokens en árbol completo               → project_replace / batch_operations
└─ varios cambios mismo archivo                      → multi_edit
```

El server ya BLOQUEA este patrón (`CheckEditRewrite`, v4.5.10). Si salta el
bloqueo, NO uses `force:true` a ciegas — replantéalo con `write_file`.

---

## REGLA 1 — Verificación previa

```
├─ read_file (start_line, end_line)                  → líneas exactas
├─ search_files (count_only:true)                    → confirmar patrón e impacto
└─ analyze_operation (operation:"edit")              → solo si MEDIUM+ (ver Regla 3b)
```

## REGLA 2 — Captura literal

```
├─ Copiar EXACTO desde read_file (espacios, tabs, saltos de línea)
├─ NO fuzzy matching — old_text es match literal, no regex
└─ Path SIEMPRE copiado de list_directory/read_file, nunca de memoria
   (capitalización, acentos, guiones vs underscore → fallos 3 capas abajo)
```

## REGLA 3 — Atómico + backup

```
├─ batch_operations → atomic:true en request_json
├─ create_backup:true
└─ Si falla → backup action:"undo_last"
```

## REGLA 3b — Dry-run CONDICIONAL (corregido)

`analyze_operation` NO es paso obligatorio. Las operaciones LOW auto-proceden
con backup y nunca se bloquean — meter dry-run en cada edit es overhead.

```
├─ Cambio LOW (puntual, pocas ocurrencias)   → directo, backup automático basta
├─ MEDIUM+ (≥30 archivos / ≥50 ocurrencias)  → analyze_operation primero
├─ ratio new/old dispara sospecha            → analyze_operation primero
└─ regex destructivo                          → edit_file dry_run:true antes
```

## REGLA 4 — Selección de herramienta

```
├─ Cambio simple                  → edit_file
├─ Múltiples cambios mismo archivo → multi_edit
├─ Múltiples archivos             → batch_operations (atomic)
├─ Rewrite total/mayoritario      → write_file        (ver Regla 0)
└─ Crítico / masivo               → analyze_operation primero
```

## REGLA 5 — Concurrencia por hash (NUEVA · sustituye validación por count)

Validar con `search(count_only)` post-edición es indirecto y no detecta
cambios externos. El server devuelve `content_hash` en cada edit.

```
├─ Cada edit_file (todos los modos) devuelve content_hash = hash post-edición
├─ Encadenar edits: pasar ese hash como expected_hash en el siguiente
│  → no hace falta re-leer entre edits
├─ --auto-occ (warn|block) detecta cambios EXTERNOS al archivo
│  (la sesión trackea sus propios writes → solo marca ediciones de terceros)
└─ Validación funcional opcional: read_file rango específico tras el cambio
```

## REGLA 6 — Recovery de backups (nunca asumir rutas)

```
├─ NUNCA asumir ruta de backup
├─ backup action:"list" [filter_path:"archivo"]     → IDs reales
├─ backup action:"info", backup_id                  → ¿existe? ¿contiene archivo? ¿ruta?
├─ backup action:"restore", preview:true            → diff previo
├─ backup action:"restore", backup_id, file_path    → restaurar
├─ backup action:"undo_last", file_path:"..."       → step-through (uno a uno)
└─ backup action:"undo_chain", file_path:"..."      → ver cadena completa
```

---

## FLUJO SEGURO (v2)

```
1. read_file(path, start_line, end_line)              → ver contenido
2. Calcular ratio new/old                              → ¿Regla 0? si sí → write_file
3. search_files(count_only:true)                       → confirmar impacto
4. Si MEDIUM+ o sospecha → analyze_operation           → dry-run
5. Aplicar:
   ├─ edit_file / multi_edit / batch_operations / write_file
   └─ guardar content_hash devuelto
6. Encadenar siguiente edit con expected_hash          → sin re-leer
7. Si algo raro → backup(action:"undo_last", file_path)
```

## ATAJOS ÚTILES

```
├─ dry_run:true en edit_file (modo regex)  → previsualizar sin aplicar
├─ multi_edit                              → varios cambios atómicos en un archivo
├─ occurrence:1 / -1                       → reemplazar solo N-ésima coincidencia
├─ expected_hash                           → encadenar edits sin re-leer
└─ force:true                              → SOLO si analyze marca CRITICAL y estás seguro
                                             (en Regla 0, force NO arregla — usa write_file)
```
