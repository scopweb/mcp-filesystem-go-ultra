# Plan de implementación: fiabilidad para IA y agentes

**Objetivo:** que las herramientas tengan un comportamiento predecible, protejan las ediciones concurrentes y devuelvan información suficiente para que un agente pueda continuar o recuperarse sin adivinar.

**Alcance:** MCP Filesystem Ultra v4.6.0. Seis fases, empezando por errores de comportamiento y terminando con evolución del contrato y evaluación con agentes reales.

**Estado:** E1–E4 (incl. 4.1) implantadas. Siguiente: **E5**.

---

## Principios

1. **Una protección aceptada siempre se aplica.** Si una combinación no está soportada, se rechaza antes de modificar archivos.
2. **Las garantías viven en el motor**, compartidas por herramientas, lotes y pipelines.
3. **Compatibilidad controlada.** Las correcciones mantienen las llamadas existentes; los cambios de formato y de semántica se introducen mediante una evolución explícita del contrato.
4. **Cada corrección incluye una prueba de regresión.**
5. **Los backups complementan la concurrencia:** no sustituyen la prevención de escrituras perdidas.

---

## Hallazgos que este plan corrige

| ID | Problema | Referencias |
|---|---|---|
| H1 | `dry_run` no se aplica en `replace_range`, `delete_range` ni `occurrence` | `tools_core.go`, `line_range.go` |
| H2 | `expected_hash` / auto-OCC no cubren todos los modos de `edit_file`; `multi_edit` no aplica `CheckAutoOCC` | `tools_core.go`, `edit_operations.go` |
| H3 | Semáforo global ≠ exclusión por archivo; TOCTOU entre hash y escritura; lectura de rango y hash en I/O distintos | `engine.go`, `edit_operations.go` |
| H4 | `--readonly` deja pasar `server_info(action:"artifact", sub_action:"write")` | `audit.go`, `tools_platform.go` |
| H5 | `search_files` no transmite `file_types`/`include` a búsqueda avanzada; `context_lines` se pierde por tipo; default de `case_sensitive` no coincide con la descripción | `tools_search.go`, `search_operations.go` |
| H6 | JSON-en-string (`edits_json`, `paths`, pipelines) frágil para agentes | handlers + `param_validator.go` |
| H7 | `performIntelligentEdit` aplica fallbacks implícitos | `edit_operations.go` |
| H8 | Salida estructurada incompleta; errores embebidos en éxitos | `output_schemas.go`, `tools_core.go` |

---

## Fase 1. Corregir las garantías actuales — P0

### 1.1 Respetar `dry_run` en todos los modos

**Implementación**

- Cubrir `replace_range`, `delete_range` y `occurrence`.
- Separar el cálculo del resultado de su escritura.
- Mientras alguna combinación no tenga simulación, devolver un error de parámetros.
- Devolver el diff previsto y distinguir el hash actual del resultado hipotético.
- Evitar cambios en archivos, backups y cadena de deshacer durante una simulación.

**Archivos**

- `internal/mcpserver/tools_core.go`
- `core/edit_operations.go`
- `core/line_range.go`

**Aceptación**

- Matriz de pruebas para todos los modos.
- `dry_run:true` conserva exactamente los bytes originales.
- El resultado previsto coincide con una aplicación posterior sobre la misma versión.

### 1.2 Aplicar OCC uniformemente

**Implementación**

- Comprobar `expected_hash` en todos los modos de edición.
- Aplicar `--auto-occ=block` también a `multi_edit` y revisar los demás recorridos de modificación.
- Rechazar la operación **antes** de crear backups o ejecutar hooks mutantes cuando exista un conflicto.
- Mantener la distinción entre hash explícito y seguimiento automático de sesión.

**Aceptación**

- Un hash desactualizado impide cualquier modificación en todos los modos.
- Un hash válido permite la operación.
- Las escrituras propias actualizan correctamente la referencia de auto-OCC.

> Esta fase corrige las omisiones actuales. La protección frente a carreras entre comprobación y escritura se completa en la fase 2.

### 1.3 Cerrar `--readonly` y corregir anotaciones

**Implementación**

- Cubrir `server_info(action:"artifact", sub_action:"write")`.
- Revisar todas las herramientas con acciones mixtas.
- Aplicar la política también en las entradas mutantes del motor.
- Corregir `readOnlyHint`, `destructiveHint` e `idempotentHint`; por ejemplo, una operación que permite añadir contenido no puede anunciar idempotencia general.

**Aceptación**

- Un servidor de solo lectura permite las consultas soportadas.
- Todas las rutas de modificación, incluidas las indirectas, quedan bloqueadas.

### 1.4 Corregir búsqueda y opciones

**Implementación**

- Introducir un `SearchOptions` tipado.
- Transmitir `file_types`, `include`, `context_lines`, límites y sensibilidad a mayúsculas.
- Aplicar filtros también en `count_only`.
- Unificar el comportamiento del motor nativo y ripgrep.
- Corregir descripciones para que coincidan con los valores predeterminados reales.

**Aceptación**

- Un filtro de extensión inexistente devuelve cero coincidencias.
- Los resultados y los conteos usan el mismo conjunto de archivos.
- `context_lines:0` no devuelve contexto.
- Ambos motores cumplen los mismos casos de prueba.

---

## Fase 2. Crear un núcleo de modificación seguro — P0/P1

Fase principal para trabajo multiagente.

### 2.1 Transacción común por archivo

Secuencia interna:

```text
Resolver y autorizar
    ↓
Adquirir coordinación sobre el destino
    ↓
Leer una instantánea y calcular su versión
    ↓
Validar precondiciones
    ↓
Calcular el cambio y ejecutar hooks aplicables
    ↓
Simular, o respaldar y escribir
    ↓
Obtener el resultado final y actualizar caché/OCC
    ↓
Liberar coordinación
```

**Migración**

1. Edición normal e inserción.
2. Rangos, ocurrencias y regex.
3. `multi_edit` y `apply_patch`.
4. Escritura, append y operaciones masivas.

**Detalles**

- El backup debe representar la versión que realmente se sustituye.
- Los errores de creación de backup deben propagarse cuando este sea obligatorio.
- Distinguir «archivo inexistente» de permisos, bloqueos u otros errores de lectura (también en `apply_patch`).
- El hash final debe corresponder a los bytes finales, incluyendo hooks y EOL.
- Las funciones internas no deben volver a adquirir un bloqueo que ya posee la transacción.

### 2.2 Coordinación concurrente

- Bloqueo por destino canónico dentro del proceso.
- Coordinación entre instancias del servidor que comparten archivos.
- Orden estable de adquisición cuando una operación afecta a varias rutas.
- Tratamiento explícito de operaciones sobre directorios y rutas solapadas.
- Esperas cancelables mediante `context.Context`.

**Límite de la garantía:** la exclusión entre servidores debe funcionar entre procesos participantes. Para editores externos que no cooperan: detectar conflicto y documentar el límite; no prometer aislamiento absoluto.

### 2.3 Lecturas coherentes

- Obtener contenido y hash de una misma instantánea.
- Aplicarlo a lectura completa, rangos, base64 y lecturas múltiples.
- Canonicalizar de forma consistente las claves de seguimiento OCC.
- Separar el estado automático por sesión efectiva; no compartir indiscriminadamente la última versión conocida entre agentes distintos.

**Aceptación de la fase**

- Dos escritores simultáneos sobre la misma versión producen un resultado válido y un conflicto, sin perder cambios silenciosamente.
- Escrituras sobre archivos independientes mantienen paralelismo.
- No hay bloqueos permanentes por operaciones anidadas.
- Contenido y hash identifican siempre la misma versión.
- Las pruebas usan barreras de sincronización, no esperas temporales frágiles.

---

## Fase 3. Integrar lotes, pipelines y recuperación — P1

- Hacer que todas las acciones mutantes utilicen el núcleo común.
- Propagar el contexto original; eliminar `context.Background()` de operaciones subordinadas que deban cancelarse.
- Construir el conjunto de rutas afectadas antes de adquirir bloqueos, cuando sea posible.
- Revisar la clasificación de acciones del scheduler, incluidas las copias que escriben destinos.
- Mantener la coordinación necesaria durante el rollback.
- Evitar que una restauración sobrescriba cambios posteriores de otro agente sin detectar el conflicto.
- Diferenciar rollback completo, parcial y fallido en el resultado.
- Definir claramente qué significa `atomic`: recuperación ante errores no equivale automáticamente a una transacción durable ante caída del proceso.

### Reintentos

Diseñar un identificador de operación independiente del ID JSON-RPC para modificaciones no idempotentes (especialmente append y lotes).

- Mismo identificador y mismos argumentos: devolver el resultado conocido.
- Mismo identificador y argumentos distintos: rechazar.
- Precisar persistencia, caducidad y comportamiento tras reiniciar el servidor **antes** de anunciar reintentos seguros.

**Aceptación**

- Un fallo intermedio no deja una modificación parcial presentada como éxito.
- El rollback conserva cambios ajenos o informa del conflicto.
- La cancelación detiene trabajo pendiente.
- Un reintento reconocido no duplica una modificación.

---

## Fase 4. Evolucionar entradas y validación — P1

### 4.1 Un único contrato de herramientas

Registro común del que obtener:

- Esquema MCP
- Validación
- Valores predeterminados
- Ayuda y ejemplos
- Clasificación de acciones

Sustituir progresivamente las definiciones paralelas de `tools_*.go` y `core/param_validator.go`.

### 4.2 Entradas nativas

Introducir arrays y objetos nativos para:

- `paths`
- `edits`
- `patterns`
- Solicitudes de lotes y pipelines

Mantener temporalmente los campos `*_json` como adaptadores. Rechazar entradas contradictorias cuando se proporcionen ambas formas.

Añadir:

- Enumeraciones para modos y acciones.
- Enteros y límites para líneas y cantidades.
- Validación de combinaciones incompatibles.
- Errores que indiquen el campo exacto y la corrección necesaria.

### 4.3 Edición estricta explícita

- Incorporar una política de coincidencia estricta.
- Mantener las tolerancias como opciones explícitas.
- Añadir `expected_matches`.
- Informar de ubicaciones candidatas cuando exista ambigüedad.
- Devolver el método de coincidencia utilizado.

**Compatibilidad:** introducir el comportamiento estricto de forma optativa y reservar su conversión en predeterminado para una versión con migración documentada.

**Aceptación**

- Las entradas antiguas y nuevas equivalentes producen el mismo resultado.
- Los ejemplos publicados se ejecutan en pruebas.
- Una opción inválida no se ignora ni cambia silenciosamente la operación.

---

## Fase 5. Resultados estructurados y control del contexto — P1/P2

Ampliar las salidas estructuradas, empezando por búsqueda, lectura múltiple, listados, backups y lotes.

| Área | Campos principales |
|---|---|
| Lectura | Ruta, contenido, hash, rango real, truncamiento |
| Búsqueda | Coincidencias, alcance, truncamiento y continuación |
| Modificación | Estado aplicado/simulado, versiones, backup, advertencias |
| Lotes | Resultado por elemento y estado agregado |
| Errores | Código, detalles, posibilidad de reintento y acción sugerida |

También:

- Propagar correctamente `isError`.
- Distinguir ausencia de resultados, fallo y resultado incompleto.
- Añadir presupuestos de respuesta y continuación para salidas grandes.
- Mantener texto legible para clientes que no consumen `structuredContent`.
- Evitar recomendar reintentar automáticamente cuando la escritura pueda haberse aplicado.

**Aceptación**

- Un cliente puede continuar sin extraer datos mediante regex del texto.
- Los errores por elemento no quedan ocultos en un éxito global.
- El truncamiento siempre es detectable.
- Todas las respuestas publicadas cumplen su esquema.

---

## Fase 6. CI, documentación y evaluación con agentes — P2

### CI y pruebas

- Ejecutar `internal/mcpserver` también en Windows.
- Usar directorios temporales únicos en todas las pruebas.
- Ampliar E2E por stdio para cubrir garantías, no solo presencia de campos.
- Mantener `go vet`, suites generales y race detector en Linux.
- Añadir un cliente de otro SDK para reducir puntos ciegos compartidos con el SDK del servidor.

### Documentación y versiones

- Generar catálogo, defaults y ejemplos desde el contrato.
- Actualizar README, ayuda, documentación web e instrucciones para agentes.
- Resolver contradicciones sobre modos, hashes, backups y número de herramientas.
- Respetar `experimental.go`: las correcciones pueden ir en una release correctiva; las ampliaciones de los 17 tools congelados requieren evolución versionada.
- Graduar las funciones experimentales con cobertura antes de publicar sus `outputSchema`.

### Evaluación con IA

Tareas reproducibles:

- Cambio localizado.
- Refactor multiarchivo.
- Recuperación de hash desactualizado.
- Escritor y revisor concurrentes.
- Reanudación tras respuesta perdida.
- Búsqueda en repositorio grande.

**Métricas:** éxito de tarea, modificaciones incorrectas, conflictos detectados, reintentos, llamadas, tokens y latencias p50/p95.

---

## Secuencia de entregas

| Entrega | Contenido | Dependencia |
|---|---|---|
| **E1** | Regresiones y correcciones de `dry_run`, OCC, `readonly` y búsqueda | Inicio |
| **E2** | Núcleo transaccional, instantáneas y coordinación | E1 |
| **E3** | Lotes, pipelines, rollback y reintentos | E2 |
| **E4** | Contrato único, entradas nativas y edición estricta optativa | E1; integración con E2 |
| **E5** | Salidas estructuradas, errores y límites de contexto | E4 |
| **E6** | Compatibilidad entre clientes, documentación y evaluación final | E3–E5 |

**Orden de ejecución:** **E1** como entrega pequeña y verificable; **E2** como trabajo arquitectónico independiente. No mezclar cambios de comportamiento, concurrencia y formatos en una sola modificación.

---

## Checklist de arranque (E1)

- [ ] 1.1 `dry_run` en `replace_range` / `delete_range` / `occurrence` + tests
- [ ] 1.2 `expected_hash` + auto-OCC en todos los modos de `edit_file` y `multi_edit` + tests
- [ ] 1.3 `--readonly` cubre artifact write + anotaciones MCP
- [ ] 1.4 `SearchOptions` tipado; filtros llegan a advanced/count; defaults documentados
- [ ] `go test ./...` y `go test -tags e2e ./tests/e2e/` en verde
