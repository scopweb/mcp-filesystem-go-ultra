# Guía mínima para IAs — filesystem-ultra

Guía de uso operativo del conector MCP **filesystem-ultra v4.6.0**.

Expone **24 herramientas**: 17 core + `git` + `minify_js` + `help` + `list_allowed_directories` + `directory_tree` + `diff_files` + `apply_patch`. Los aliases antiguos y el super-tool `fs` están deshabilitados. `directory_tree` sí está registrado (experimental).

## Reglas obligatorias

1. **Usar una sola familia de herramientas por proyecto.**
   - Llamar `list_allowed_directories` antes de la primera lectura para ver las raíces del sandbox.
   - Para proyectos accesibles mediante filesystem-ultra, usar sus herramientas para leer, buscar, escribir, editar, listar y borrar.
   - No mezclarlo silenciosamente con herramientas de otro MCP o de un filesystem sandbox.
   - Si un archivo conocido devuelve `NOT_FOUND` / `File not found`, comprobar primero que no se está usando otro filesystem.

2. **Copiar las rutas exactamente desde `list_directory`, `search_files` o `read_file`.**
   - No reconstruirlas de memoria.
   - Respetar mayúsculas, acentos, guiones y underscores, incluso en Windows.

3. **Leer antes de editar.**
   - Obtener el texto exacto y, cuando esté disponible, el `content_hash` de `read_file`.
   - Enviar ese hash como `expected_hash` a `edit_file` o `multi_edit` para detectar cambios externos.
   - El hash está en `structuredContent`; no debe buscarse dentro del contenido textual del archivo.

4. **Elegir la operación según la intención.**
   - Archivo completo: `write_file`.
   - Cambio pequeño: `edit_file`.
   - Varios cambios independientes en un archivo: `multi_edit`.
   - Reemplazo en un árbol completo: `project_replace`.
   - Operaciones coordinadas entre varios archivos: `batch_operations`.

5. **No usar `edit_file` para reescribir un archivo completo.**
   - `edit_file` sustituye únicamente el fragmento encontrado; no elimina el resto del archivo.
   - Si `new_text` contiene la mayor parte o la totalidad del archivo, usar `write_file`.

6. **Previsualizar los cambios amplios o destructivos.**
   - `edit_file` / `multi_edit`: `dry_run:true`.
   - `project_replace`: `preview:true`.
   - Pipeline: `dry_run:true`.
   - Lote atómico: `validate_only:true`.
   - Antes de un reemplazo global, usar `search_files` con `count_only:true`.

7. **Verificar después de cada mutación.**
   - Confirmar existencia y tamaño con `get_file_info` o `list_directory`.
   - Usar `read_file` cuando importe verificar el contenido.
   - No considerar suficiente una respuesta de escritura exitosa.

8. **Conservar la recuperación.**
   - `edit_file` y `multi_edit` generan backups automáticos.
   - Guardar el `backup_id` o `parent_backup_id` devuelto en `structuredContent` cuando se necesite una cadena de recuperación.
   - Usar `backup(action:"undo_last")`, `undo_chain` o `restore` para recuperar versiones.

---

## Escritura y edición

### `write_file`

Crear un archivo o sobrescribirlo completamente de forma atómica.

Usarlo cuando:
- el archivo no existe;
- se genera su contenido completo;
- se reescribe la mayor parte del archivo;
- se escribe contenido binario en base64;
- se concatena al final: `mode:"append"` (no dispara rewrite-guard).

No sustituir una reescritura completa por un `edit_file` con un anchor pequeño.

### `edit_file`

Aplicar un cambio puntual. Crea backup automático y devuelve el hash posterior.

Modos principales:
- `replace`: reemplazo exacto y único;
- `search_replace`: reemplazo de todas las coincidencias;
- `regex`: transformaciones regex con capturas;
- `replace_range`: sustituir líneas `start_line..end_line`;
- `delete_range`: eliminar líneas `start_line..end_line`.

Parámetros de seguridad recomendados:
- `expected_hash`: hash de la lectura o escritura anterior;
- `dry_run:true`: previsualizar sin escribir;
- `occurrence`: seleccionar una coincidencia concreta cuando corresponda.

Tras una edición exitosa, el `content_hash` retornado puede enviarse como `expected_hash` en la siguiente edición.

### `multi_edit`

Aplicar múltiples reemplazos en un mismo archivo de forma atómica.

Reglas:
- todos los cambios deben ser **independientes** y poder evaluarse contra el contenido original;
- si un cambio depende del resultado de otro, usar llamadas consecutivas a `edit_file`, encadenando `content_hash` → `expected_hash`;
- ante un reemplazo fallido o ambiguo, la operación revierte el lote completo;
- admite `expected_hash` y `dry_run:true`.

### `project_replace`

Buscar y reemplazar en un árbol de proyecto con una sola operación.

Usarlo para:
- renombrar tokens en muchos archivos;
- migrar APIs o nombres;
- reemplazar texto literal o regex por tipo de archivo.

Flujo recomendado:
1. ejecutar con `preview:true`;
2. revisar `files_changed`, `total_replacements` y el diff;
3. ejecutar sin preview manteniendo `create_backup:true`.

Opciones útiles: `literal`, `case_sensitive`, `file_types`, `include_paths`, `exclude_paths`, `max_files` y `parallel`.

### `batch_operations`

Coordinar operaciones multiarchivo con atomicidad, backup y rollback.

Usarlo para:
- escribir, editar, copiar, mover o borrar varios archivos juntos;
- crear directorios;
- extraer un rango de líneas de un archivo a otro;
- ejecutar pipelines y batch rename.

Tipos válidos del lote atómico:
- `write`;
- `edit`;
- `search_and_replace`;
- `copy`;
- `move`;
- `delete`;
- `create_dir`;
- `extract`.

No inventar nombres como `search_replace` o `find_replace` dentro de `request_json`.

---

## Lectura y búsqueda

### `read_file`

Leer:
- un archivo completo;
- múltiples archivos mediante `paths`;
- un rango de líneas;
- las primeras o últimas líneas (`head` / `tail`);
- logs: `mode:"tail"` + `max_lines:40` (cada línea se corta a 300 chars, equivalente a `tail | cut`; `max_line_length:0` lo desactiva);
- binarios en base64.

Las lecturas individuales completas, por rango o base64 pueden devolver `content_hash` en `structuredContent`. Una lectura batch de varios archivos no proporciona un único hash utilizable para editar todos ellos.

### `list_allowed_directories`

Cero parámetros. Devuelve las raíces efectivas del sandbox. Llamarla antes de la primera lectura.

### `list_directory`

Listar contenido de directorios usando caché.

Formatos útiles:
- `compact`: respuesta corta;
- `json`: entradas estructuradas;
- `tree`: árbol recursivo con profundidad controlada.

Usarlo también para copiar rutas con su capitalización exacta.

### `directory_tree`

Árbol compacto (experimental). Equivale a `list_directory` con `output_format:"tree"`. Respeta `.gitignore`. `max_depth` por defecto 2.

### `diff_files`

Diff unificado entre dos rutas (`path_a`, `path_b`) o un archivo contra su último backup (`path` + `against:"backup"`).

### `apply_patch`

Aplicar un unified diff a **un** archivo. Fail-closed: sin fuzzy, sin multi-file. `dry_run:true` previsualiza. `expected_hash` para OCC.

### `search_files`

Buscar por nombre o contenido.

Opciones comunes:
- `include_content:true`: buscar dentro de archivos;
- `count_only:true`: contar coincidencias antes de reemplazar;
- `file_types` o `include`: filtrar por glob/tipo;
- `case_sensitive`;
- `include_context:true`;
- `max_results`;
- `output_format` o `output`: `content`, `files_with_matches` o `count`.

---

## Operaciones de archivos

### `get_file_info`

Obtener metadata de un archivo o directorio. Admite `paths` para consultas batch.

Usarlo para verificar existencia, tamaño y modificación después de escribir o editar.

### `copy_file`

Copiar un archivo o directorio a otro destino.

### `move_file`

Mover o renombrar un archivo o directorio.

### `delete_file`

Eliminar archivos o directorios.

- Por defecto realiza **soft-delete**.
- `permanent:true` realiza un borrado irreversible y debe usarse sólo con autorización explícita.
- Para múltiples rutas puede utilizar `paths`.

### `create_directory`

Crear un directorio y sus padres, equivalente a `mkdir -p`.

---

## Análisis y recuperación

### `analyze_operation`

Analizar impacto o riesgo antes de una operación de archivo, optimización, escritura, edición o borrado.

Es un analizador preventivo; para ver el diff exacto de una edición debe preferirse el dry-run de la propia herramienta de edición.

### `backup`

Administrar backups y papelera.

Acciones principales:
- `list` / `info` / `compare`;
- `restore`;
- `undo_last`;
- `undo_chain`;
- `cleanup`;
- `list_trash` / `restore_trash` / `purge_trash`.

Recuperación recomendada:
- último cambio: `backup(action:"undo_last", file_path:"...")`;
- inspeccionar la cadena: `backup(action:"undo_chain", file_path:"...")`;
- versión concreta: `backup(action:"restore", backup_id:"ID_COMPLETO")`.

---

## Utilidades

### `git`

Herramienta única para operaciones Git:
- `init`;
- `status`;
- `diff`;
- `log`;
- `show`;
- `add`;
- `commit`;
- `restore`;
- `branch`.

Reglas mínimas:
- pasar una ruta situada dentro del repositorio;
- `paths` es un array nativo, no un JSON serializado dentro de un string;
- usar `output:"stat"`, `"name-only"` o `"full"` según el volumen necesario;
- previsualizar `restore` cuando corresponda;
- no ejecutar `commit`, `restore` o cambios de ramas sin autorización explícita.

### `minify_js`

Minificar JavaScript en Go puro, sin depender de Node.js.

- usar `dry_run:true` para previsualizar;
- usar `output_path` para conservar el archivo original;
- mantener backup al sobrescribir in-place.

### `wsl`

Sincronizar archivos y convertir rutas entre WSL y Windows. Comprobar primero la dirección de sincronización para no sobrescribir el lado incorrecto.

### `server_info`

Consultar estadísticas, configuración, introspección y ayuda temática del servidor.

Para instrucciones detalladas puede usarse `action:"help"` con un `topic` como `workflow`, `edit` o `recovery`.

### `help`

Punto de descubrimiento básico del conector. Si el cliente sólo recibe una instrucción breve, usar `server_info(action:"help", topic:"...")` para ayuda temática y esta guía como catálogo operativo.

---

## Flujo mínimo recomendado para una IA

```text
1. Identificar que el proyecto pertenece al filesystem accesible por filesystem-ultra.
2. list_allowed_directories — ver raíces del sandbox.
3. Obtener la ruta exacta con list_directory/directory_tree/search_files.
4. Leer el archivo con read_file y conservar content_hash.
5. Elegir write_file, edit_file, multi_edit, apply_patch, project_replace o batch_operations.
6. Hacer dry-run/preview si el impacto es amplio.
7. Ejecutar pasando expected_hash cuando aplique.
8. Verificar con get_file_info y, si importa el contenido, read_file.
9. Conservar backup_id para una posible restauración.
```

## Resumen de decisión

| Necesidad | Herramienta |
|---|---|
| Ver raíces del sandbox | `list_allowed_directories` |
| Árbol compacto | `directory_tree` |
| Diff de dos archivos o vs backup | `diff_files` |
| Aplicar unified diff (1 archivo) | `apply_patch` |
| Crear o reescribir archivo completo | `write_file` |
| Concatenar al final | `write_file` `mode:"append"` |
| Cambio pequeño y localizado | `edit_file` |
| Varios cambios independientes en un archivo | `multi_edit` |
| Reemplazo en todo el proyecto | `project_replace` |
| Operaciones atómicas entre varios archivos | `batch_operations` |
| Leer contenido o líneas | `read_file` |
| Listar y confirmar rutas | `list_directory` |
| Buscar nombres o contenido | `search_files` |
| Verificar metadata | `get_file_info` |
| Copiar | `copy_file` |
| Mover o renombrar | `move_file` |
| Borrar con recuperación | `delete_file` |
| Crear directorios | `create_directory` |
| Analizar riesgo | `analyze_operation` |
| Restaurar o deshacer | `backup` |
| Operar con Git | `git` |
| Minificar JavaScript | `minify_js` |
| Trabajar con WSL | `wsl` |
| Consultar servidor/ayuda temática | `server_info` |
| Descubrimiento básico | `help` |
