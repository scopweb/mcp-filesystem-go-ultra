# Plan de arreglos de usabilidad de filesystem-ultra

Fecha: 2026-10-14  
Estado: bloques 1, 2 y 4 implementados y probados en local. Bloque 3 no verificado en Claude Code. Bloque 5 parcial: skill del repo y C:\MCPs\SKILLS actualizadas; binario activo no reiniciado.

## Objetivo

Corregir la semántica de las lecturas parciales, reducir respuestas redundantes y mejorar la experiencia de Claude Code mediante el puente remoto. Mantener coherentes el servidor, sus contratos de salida y las skills instaladas.

## Contexto confirmado

- Ambos servidores declaran v4.8.0, commit `dec6c03`, build_date `2026-10-13`.
- El cliente que reportó las incidencias utiliza perfil `ultra`, 30 herramientas, modo de raíces `union` y escritura habilitada.
- Ese cliente recibió una búsqueda de 116 coincidencias duplicadas en `matches` y `message`.
- Sus lecturas llegaron como JSON con el contenido escapado. Está pendiente localizar la transformación exacta en servidor, puente o cliente.
- El código marca como truncado un rango completo si no abarca todo el archivo.
- Se reportaron raíces repetidas dentro de `paths`. El código permite que entradas diferentes converjan en la misma ruta mostrada después de normalizarse; falta confirmar las entradas originales del caso.
- El repositorio local contiene cambios posteriores y trabajo pendiente. Revisar el estado y los diffs antes de implementar para integrar lo existente.
- La copia `C:\MCPs\SKILLS\filesystem-ultra-tools\SKILL.md` está desactualizada: menciona 17 herramientas y recomienda cargar el catálogo completo. No menciona v4.7.1 ni `--profile=strict`, por lo que no está identificada como la copia cargada por el otro cliente.

## Orden de ejecución

1. Corregir truncado y continuación de `read_file`.
2. Eliminar duplicación interna de respuestas estructuradas.
3. Reproducir y mejorar la presentación de lecturas en Claude Code con el puente remoto.
4. Aclarar el ámbito de búsqueda y eliminar duplicados en las raíces mostradas.
5. Sincronizar documentación, validar y comprobar la entrega desplegada.

Primer bloque recomendado de implementación: puntos 1 y 2, localizados y verificables directamente.

## 1. Corregir `read_file`: rango solicitado frente a truncado

**Archivos principales:** `internal/mcpserver/format.go`, `internal/mcpserver/e5.go` y pruebas de lectura.

- [x] Devolver `status:"ok"` y `truncated:false` cuando se entregue completo el rango solicitado.
- [x] Reservar `truncated:true` para recortes efectivos por límites.
- [x] Mantener `start_line`, `end_line` y `total_lines` como información del rango.
- [x] Mostrar continuación cuando haya contenido solicitado pendiente por un recorte; eliminar la sugerencia automática al completar un rango.
- [x] Precisar la semántica de `head`, `tail` y `max_lines`, distinguiendo una selección explícita de un límite impuesto por el servidor.
- [x] Verificar rangos intermedios, final de archivo, archivo vacío, rangos fuera del archivo, `head`/`tail`, límites automáticos y límites de longitud de línea.

**Criterio de aceptación:** pedir las líneas 82–85 y recibirlas completas no se presenta como una respuesta incompleta ni solicita continuar. Un recorte efectivo sigue siendo identificable; cualquier continuación debe ser válida para el tipo de recorte.

## 2. Eliminar duplicación en respuestas estructuradas

**Archivos principales:** `internal/mcpserver/e5.go`, `internal/mcpserver/detail.go`, `internal/mcpserver/tools_discovery.go` y esquemas/pruebas correspondientes.

- [x] En `search_files`, conservar las coincidencias en `matches` y convertir `message` en un resumen breve.
- [x] En `list_allowed_directories`, conservar las raíces en `paths` y usar un resumen breve en `message`.
- [x] Mantener una salida textual útil para clientes que consuman esa representación.
- [x] Revisar contratos y pruebas que asuman que `message` contiene todo el texto alternativo.
- [x] Comprobar `summary`, `normal`, `full`, resultados vacíos, conteos y paginación.
- [x] Comparar el tamaño serializado antes/después con una búsqueda reproducible de 116 coincidencias.

**Criterio de aceptación:** el objeto estructurado no contiene dos copias completas de los resultados. Medir bytes y describir el alcance de la medición; el ahorro de tokens depende de la representación que el cliente envíe al modelo.

## 3. Comprobar y mejorar la lectura en Claude Code con el puente remoto

**Ámbito:** respuesta de `read_file` y transformación realizada por el puente.

- [ ] Capturar una lectura pequeña con CRLF, comillas y un rango conocido. No hay captura del cliente Claude Code.
- [x] Comparar la respuesta MCP original con lo que el puente entrega a Claude Code. El proxy local (`cmd/proxy`) reenvía la línea MCP sin transformarla (`writeClient(line)`).
- [x] Localizar dónde se selecciona o serializa el contenido como JSON escapado. No es el proxy. El servidor publica `structuredContent.content` y un bloque de texto. Un cliente que serializa el objeto estructurado escapa comillas y saltos.
- [x] Elegir el arreglo según esa evidencia: presentación del puente o una opción compatible de salida del servidor. No se añade modo solo-texto: rompería `outputSchema`. El texto ya es el cuerpo legible.
- [x] Evaluar la compatibilidad con el esquema de salida antes de introducir una opción de solo texto.
- [x] Incorporar numeración opcional donde realmente la consuma el cliente, evitando añadir otra copia completa del archivo. No se añade: no hay evidencia de que ese cliente la consuma, y un prefijo contaminaría `edit_file`.
- [ ] Verificar que los prefijos de presentación no se confundan con el contenido utilizado para editar y que se conserve el hash del snapshot original. El hash del snapshot completo se conserva en pruebas locales; la integración remota no está comprobada.

**Criterio de aceptación:** lectura legible y líneas identificables en esa integración, preservando el contenido y el hash usados para editar. La numeración por sí sola no resuelve el coste ni la legibilidad del JSON escapado.

**Dependencia:** hace falta acceso a esa ruta de cliente/puente o una captura de sus respuestas para validar el resultado completo. No dar esta integración por verificada solo con pruebas del servidor.

## 4. Aclarar búsquedas y normalizar raíces mostradas

**Archivos principales:** `internal/mcpserver/tools_search.go`, `core/roots.go`, `core/engine.go` y pruebas.

### 4.1. Ámbito de búsqueda

- [x] Revisar los cambios locales existentes: durante la revisión ya apareció un mensaje vacío que aclaraba que la búsqueda era por nombre.
- [x] Indicar el ámbito en resultados vacíos: «0 coincidencias en nombres de fichero» o «0 coincidencias en contenido».
- [x] Verificar las reglas actuales que infieren búsqueda de contenido a partir de una ruta de archivo u otros parámetros.
- [x] Evaluar `mode:"name"|"content"` opcional como ampliación separada, con reglas explícitas ante conflictos con `include_content` y respetando la política de estabilidad del proyecto. No se añade: `search_files` es herramienta core congelada; el ámbito vacío y `scope.searched` cubren el criterio sin un parámetro nuevo.
- [x] Mantener compatibilidad con llamadas existentes; no inferir la intención únicamente por la presencia de una regex.

**Criterio de aceptación:** una búsqueda vacía deja inequívoco qué ámbito se examinó. Si se añade `mode`, su precedencia y compatibilidad quedan documentadas y probadas.

### 4.2. Raíces mostradas

- [x] Revisar las raíces originales de configuración y del cliente cuando estén disponibles para reproducir el caso reportado. La deduplicación es de presentación; no se reconstruyó el volcado original del cliente.
- [x] Eliminar duplicados de la lista mostrada después de normalizar rutas y resolver enlaces.
- [x] Usar comparación acorde con el sistema operativo.
- [x] Probar separadores, barra final, mayúsculas en Windows y enlaces cuando el entorno permita crearlos.
- [x] Conservar raíces distintas aunque una esté contenida en otra.
- [x] Verificar que la deduplicación de presentación no altere el alcance de autorización.

**Criterio de aceptación:** cada raíz efectiva aparece una sola vez en `paths`, sin fusionar rutas distintas ni modificar los permisos efectivos.

## 5. Sincronizar documentación y validar la entrega

**Ámbito:** skill del repositorio, copias instaladas identificadas y binario utilizado.

- [x] Actualizar ejemplos, semántica de truncado y formatos de respuesta.
- [x] Evitar inventarios fijos como descripción de la sesión: consultar `profile`, `tool_count` y herramientas efectivamente expuestas.
- [x] Actualizar la copia antigua de `C:\MCPs\SKILLS\filesystem-ultra-tools\SKILL.md`.
- [x] Localizar la copia que el otro cliente carga como v4.7.1 antes de sincronizarla. La referencia en `.claude/skills/filesystem-ultra-tools/ripgrep-reference.md` seguía en v4.7.0; actualizada a v4.8.0 y copiada a `C:\MCPs\SKILLS\filesystem-ultra-tools`. `C:\Users\DAVID\.claude` sigue fuera del sandbox.
- [x] Alinear la guía de descubrimiento con `list_allowed_directories` primero y ayuda específica bajo demanda.
- [x] Ejecutar pruebas de regresión de los cambios y contratos de salida; después las comprobaciones generales pertinentes.
- [x] Compilar el servidor desde `./cmd/filesystem-ultra`. Salida de comprobación: `%TEMP%\filesystem-ultra-usability.exe`. No sustituye el binario en uso.
- [x] Identificar el ejecutable realmente configurado antes de desplegar. OpenCode usa `bin\filesystem-ultra-v4-embed_rg.exe` detrás de `bin\mcp-proxy.exe`.
- [ ] Tras desplegar y reiniciar, comprobar versión, commit y fecha de compilación, y repetir las llamadas representativas desde el cliente afectado.
- [ ] Recargar la skill en una sesión nueva y verificar su contenido efectivo.

**Criterio de aceptación:** código, binario activo y skill cargada son coherentes. Distinguir explícitamente las verificaciones locales de las realizadas en el cliente remoto.

## Segunda pasada

- [ ] Reducir rutas absolutas repetidas en los diffs, definiendo una base inequívoca para rutas relativas.
- [ ] Revisar y probar la información ahead/behind del resumen compacto de Git.

Estos puntos tienen menor prioridad. El porcelain v2 observado no constituye una incidencia: el cliente había pedido `output:"full"`. Las diferencias de sintaxis entre remotos SSH tampoco demuestran un fallo.

## Comprobaciones de cierre

- [x] Rango completo solicitado: estado correcto y sin continuación innecesaria. Verificado en local.
- [x] Recorte real: indicador correcto y metadatos útiles. Verificado en local.
- [x] Búsquedas y descubrimiento: resultados estructurados sin repetición completa en `message`. 116 hits: 17882 bytes frente a 20301 con `message` duplicado (11,9 % del objeto JSON, no del tokenizador).
- [ ] Claude Code y puente remoto: presentación comprobada con evidencia de esa integración.
- [x] Raíces equivalentes: una entrada mostrada por raíz efectiva. Verificado en local; la autorización no cambia.
- [ ] Documentación y skill efectiva: actualizadas y recargadas. Archivos actualizados; la sesión que las carga no se ha recargado.
- [ ] Identidad del binario desplegado: comprobada después del reinicio.
