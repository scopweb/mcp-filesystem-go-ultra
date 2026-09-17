# Plan de implantación: fiabilidad operativa

Estado: implantado (código + pruebas). Evaluación real con modelo sigue abierta en PLAN-PENDIENTE.md.  
Fecha: 2026-09-17  
Base: código revisado de v4.7.0; verificar binario y esquema del incidente.

## Objetivo

Priorizar respuestas completas, inequívocas y recuperables.
Después, medir ahorro de tokens y latencia mediante la evaluación real
pendiente en PLAN-PENDIENTE.md.

El informe de campo aporta casos de uso y síntomas. Las reproducciones
sobre un binario identificado determinan los defectos que deben corregirse.

## Orden obligatorio

1. Reproducir versión, binario y esquema.
2. Corregir diagnóstico de rollback y mostrar el destino Git.
3. Resolver truncamiento y paginación de búsqueda.
4. Incorporar consulta de remotos y allowlist de destinos efectivos.
5. Corregir telemetría y ejecutar evaluación real.

Cada entrega debe cerrar sus criterios de aceptación antes de avanzar.

## Situación de partida

v4.7.0 ya incluye:
- Exclusión de push/fetch del enum cuando --git-network está desactivado.
- Rollback atómico de multi_edit.
- detail en varias herramientas.
- truncated y hidden_count en la salida estructurada de búsqueda.

Persisten tres defectos prioritarios:
- push compacto no muestra la URL del destino.
- search_files recorta la presentación aunque se soliciten más resultados.
- Los edits ambiguos incrementan FailedEdits, pero pueden desaparecer del
  diagnóstico y producir "Failed: .".

No interpretar:
- Backups persistentes como ediciones de la sesión actual.
- Contadores de operaciones internas como llamadas MCP sin verificar
  su definición.
- Impresiones de rapidez como mediciones de rendimiento.

---

## Entrega 1 — Reproducción y trazabilidad

### Trabajo

- [ ] Identificar ejecutable, versión, commit y fecha de compilación.
- [ ] Registrar opciones relevantes: profile, compact-mode, git-network
      y configuración de logs.
- [ ] Capturar tools/list del servidor que utiliza el cliente.
- [ ] Verificar el esquema con git-network activado y desactivado.
- [ ] Preparar una reproducción de multi_edit con un edit válido y otro
      ambiguo.
- [ ] Preparar una reproducción de push compacto con destino controlado.
- [ ] Preparar búsquedas con más de 20 resultados y max_results explícito.
- [ ] Clasificar cada incidencia: reproducida, ya mitigada o pendiente
      de evidencia del cliente.

### Evidencia mínima por caso

- Identidad del binario y configuración.
- Petición exacta.
- Respuesta de texto y contenido estructurado.
- Resultado esperado.
- Estado del fichero o repositorio antes y después, cuando corresponda.

### Aceptación

- [ ] Los tres defectos prioritarios tienen una reproducción mínima.
- [ ] Las diferencias de versión y esquema no se confunden con defectos
      del código actual.
- [ ] Las pruebas de red utilizan repositorios controlados o un ejecutor
      simulado.

---

## Entrega 2 — Rollback inequívoco y destino visible

### 2.1 Diagnóstico de multi_edit

Archivos principales:
- core/edit_operations.go
- internal/mcpserver/tools_batch.go

Trabajo:
- [ ] Incluir todos los estados que incrementan FailedEdits, especialmente
      ambiguous, en el diagnóstico del motor y del handler.
- [ ] Informar índice, estado y causa del edit problemático.
- [ ] Distinguir ediciones simuladas de cambios escritos.
- [ ] Mantener el diagnóstico completo aunque no exista backup.
- [ ] Conservar la atomicidad y los identificadores de recuperación.

Pruebas:
- [ ] Edit válido seguido de edit ambiguo.
- [ ] Edit válido seguido de texto no encontrado.
- [ ] Todos los edits fallidos.
- [ ] Mezcla de already_present, ambiguous y failed.
- [ ] Comparación byte a byte del fichero tras el rechazo.

Aceptación:
- [ ] Ningún caso produce "Failed: ." ni una lista vacía de causas.
- [ ] Cada fallo contabilizado tiene un diagnóstico identificable.
- [ ] El fichero permanece intacto cuando se rechaza el conjunto.

### 2.2 Destino efectivo de Git

Archivo principal:
- internal/mcpserver/tools_git.go

Trabajo:
- [ ] Resolver el destino efectivo de push y fetch.
- [ ] Mostrarlo tanto en compacto como en salida larga.
- [ ] Contemplar pushurl, destinos múltiples y reescrituras de URL.
- [ ] Ocultar credenciales en respuestas, errores y auditoría.
- [ ] Extraer un resolvedor reutilizable para la entrega 4.

Aceptación:
- [ ] El resultado identifica el destino efectivo y no solo "origin".
- [ ] Las credenciales no aparecen en ninguna salida.
- [ ] La documentación distingue visibilidad posterior de prevención.

### 2.3 Ayuda y mensajes

- [ ] Retirar la sugerencia inexistente verbose=true de project_replace.
- [ ] Aclarar las combinaciones detail y count_only.
- [ ] Documentar cálculo y carácter informativo del risk de git commit.
- [ ] Comprobar que los ejemplos solo recomiendan parámetros admitidos.

---

## Entrega 3 — Búsqueda completa y paginada

Archivos principales:
- core/search_options.go
- core/search_outcome.go
- core/search_operations.go
- internal/mcpserver/tools_search.go
- internal/mcpserver/output_schemas.go

### Diseño previo

- [ ] Definir qué representa una coincidencia: línea coincidente u
      ocurrencia individual.
- [ ] Documentar las unidades de max_results según la modalidad.
- [ ] Elegir cursor u offset y justificar la decisión.
- [ ] Definir el comportamiento si el árbol cambia entre páginas.
- [ ] Definir un orden estable de resultados.
- [ ] Distinguir exclusiones por filtros de resultados pendientes
      por límites de respuesta.

### Trabajo

- [ ] Eliminar los topes ocultos de presentación de 10/20 resultados
      que contradicen el límite solicitado.
- [ ] Añadir continuación explícita cuando no quepa la respuesta completa.
- [ ] Aplicar el presupuesto tanto al texto como al contenido estructurado.
- [ ] Informar resultados devueltos, truncamiento y motivo.
- [ ] No anunciar un total exacto si la exploración terminó antes.
- [ ] Mantener coherencia entre texto y structuredContent.
- [ ] Presentar contexto ordenado y numerado.
- [ ] Ofrecer salida compacta sin decoración innecesaria.

### Pruebas

- [ ] Resultados por debajo, en el límite y por encima del tamaño de página.
- [ ] Muchas coincidencias en un único fichero.
- [ ] Coincidencias repartidas entre muchos ficheros.
- [ ] Límites de bytes, líneas largas, Unicode y CRLF.
- [ ] Recorrido completo de páginas sobre un árbol sin cambios.
- [ ] Cambio del árbol entre páginas según el contrato definido.
- [ ] Modos compacto, texto, JSON y detail.

### Aceptación

- [ ] Todas las coincidencias son recuperables sin reformular la búsqueda.
- [ ] Con el árbol sin cambios no hay pérdidas ni duplicados.
- [ ] Cada recorte informa cómo continuar.
- [ ] La salida estructurada también respeta el presupuesto.
- [ ] El contexto identifica inequívocamente la línea coincidente.

---

## Entrega 4 — Consulta de remotos y prevención

Archivos principales:
- internal/mcpserver/tools_git.go
- internal/mcpserver/run.go
- Configuración, ayuda y pruebas correspondientes.

### Consulta de solo lectura

- [ ] Añadir git(action:"remote").
- [ ] Permitirla sin --git-network.
- [ ] Mostrar nombres y destinos efectivos de fetch/push.
- [ ] Reutilizar la resolución y ocultación de credenciales de la entrega 2.

### Allowlist

- [ ] Definir sintaxis y alcance: host, namespace o repositorio.
- [ ] Documentar comportamiento con allowlist ausente.
- [ ] Validar destinos efectivos antes de ejecutar operaciones de red.
- [ ] Contemplar pushurl, reescrituras y destinos múltiples.
- [ ] Rechazar el conjunto si cualquier destino no está autorizado.
- [ ] No permitir que force:true eluda esta política.
- [ ] Especificar protocolos y formatos admitidos.
- [ ] Documentar límites de la comprobación ante cambios concurrentes
      de configuración.

### Pruebas

- [ ] Remoto permitido y remoto rechazado.
- [ ] origin con URL cambiada.
- [ ] URL de fetch permitida y pushurl rechazada.
- [ ] Varios destinos con uno no autorizado.
- [ ] HTTPS, SSH y formato SCP según soporte.
- [ ] URLs reescritas.
- [ ] Credenciales ocultas.
- [ ] Rechazo anterior a cualquier ejecución de red.

### Aceptación

- [ ] El nombre del remoto no basta para autorizar el envío.
- [ ] Un destino no autorizado se rechaza antes de contactar con él.
- [ ] El usuario puede consultar el destino sin habilitar la red.

### Fuera de esta entrega

La protección general de escrituras en .git requiere una política
independiente y una revisión de compatibilidad. No condiciona el cierre
de la prevención de destinos.

---

## Entrega 5 — Telemetría verificable y evaluación real

Archivos principales:
- core/engine.go
- core/audit_logger.go
- internal/mcpserver/audit.go
- internal/mcpserver/tools_platform.go
- cache/intelligent.go
- PLAN-PENDIENTE.md

### Definición de métricas

- [ ] Separar llamadas MCP de operaciones internas.
- [ ] Separar mutaciones aplicadas, rechazadas y simuladas.
- [ ] Separar backups persistentes de actividad del proceso.
- [ ] Identificar proceso, uptime y periodo de medición.
- [ ] Definir cómo se cuentan errores, suboperaciones y la propia
      consulta de estadísticas.

### Correcciones

- [ ] Calcular ops/s mediante diferencias de contadores por intervalo.
- [ ] Calcular la media de latencia mediante suma y número de muestras.
- [ ] Definir puntos de inicio/fin y tratamiento del tiempo de espera.
- [ ] Cubrir multi_edit, project_replace, parches y lotes.
- [ ] Verificar hits/misses con lecturas elegibles repetidas.
- [ ] Añadir p50/p95 con una estrategia explícita de muestreo.
- [ ] Evitar dobles conteos entre handler y motor.

### Aceptación técnica

- [ ] Una secuencia conocida produce contadores reconciliables.
- [ ] Una segunda lectura elegible, sin cambios, registra cache hit.
- [ ] Rechazos y dry-runs no se contabilizan como escrituras aplicadas.
- [ ] Los backups históricos no se presentan como edits de sesión.
- [ ] El reinicio del proceso tiene un efecto documentado.

### Evaluación real

Integrar con el trabajo ya abierto en PLAN-PENDIENTE.md.

- [ ] Ejecutar tareas equivalentes antes y después.
- [ ] Registrar modelo, cliente, versión y configuración.
- [ ] Medir corrección, éxito de tarea, llamadas y reintentos.
- [ ] Medir tokens de entrada/salida y latencia total.
- [ ] Incluir tareas de búsqueda, edición múltiple y reemplazo masivo.
- [ ] Publicar resultados y limitaciones del experimento.

Aceptación:
- [ ] Las afirmaciones sobre ahorro y velocidad están respaldadas
      por mediciones reproducibles.

---

## Reglas transversales de implantación

- Preservar los cambios existentes del usuario.
- Mantener las correcciones separadas de las nuevas capacidades.
- Seguir la política experimental vigente para nuevos modos y acciones.
- Actualizar esquema, handler, ayuda y pruebas en la misma entrega.
- Preservar compatibilidad del contrato estable o documentar su migración.
- Añadir pruebas de regresión sobre causas reales.
- Verificar contenido y existencia después de cada modificación.
- No realizar push ni modificar remotos reales para probar la funcionalidad.

## Verificación antes de cerrar cada entrega

- [ ] Pruebas específicas de los cambios.
- [ ] Pruebas de integración y contratos afectados.
- [ ] Suite relevante de core e internal/mcpserver.
- [ ] Suite de seguridad cuando cambien autorización o destinos.
- [ ] Compilación del servidor desde ./cmd/filesystem-ultra.
- [ ] Revisión del diff y actualización de documentación.

## Asuntos posteriores, fuera de la secuencia principal

- Detalle opcional de ficheros afectados en project_replace.
- Política integral de escritura sobre metadatos .git.
- Investigación adicional de STALE_READ si la reproducción demuestra
  que persiste en el binario actual.

## Criterio global de finalización

El agente puede:
1. Identificar por qué falló cada edición.
2. Recuperar todos los resultados de una búsqueda.
3. Consultar y verificar el destino de una operación Git de red.
4. Interpretar las métricas sin mezclar proceso, sesión e histórico.

Solo después se consideran justificadas las conclusiones de ahorro
de tokens y mejora de latencia.