# Plan de workspace experimental

## 1. Estado y propósito
- Estado: MVP implementado (`begin/status/diff/export/discard`). `promote` y `query` siguen fuera.
- La aprobación original no declaraba la función entregada; el código y las pruebas de `internal/mcpserver` son ahora la autoridad del MVP.
- Objetivo: preparar cambios en un árbol de trabajo separado, compilable y revisable.
- Exposición propuesta: una herramienta `workspace`, experimental y exclusiva de `ultra`.
- MVP: `begin`, `status`, `diff`, `export` y `discard`.
- `promote` y `query` quedan diferidos, fuera del MVP.

## 2. Condiciones previas de alcance
- `HOJA-DE-RUTA.md` excluye actualmente overlay / begin-promote de forma expresa.
- Implementar este plan exige acordar después un cambio de alcance en esa hoja de ruta.
- Este documento no modifica la hoja de ruta ni resuelve esa decisión por anticipado.
- Referencia de la revisión recibida: `serverVersion` 4.8.0.
- En esa revisión, `context_pack` era el único experimental y `github_issues` ya estaba graduado.
- Revalidar versión, registro experimental, perfiles y contratos al iniciar implementación.
- Los archivos y Git vigentes son autoridad; un handoff antiguo no acredita el estado actual.
- La versión de introducción será la release acordada; no se presupone una versión 4.9.

## 3. Modelo de aislamiento
- El workspace será un worktree Git real, con archivos visibles a compiladores y herramientas.
- No es un sandbox de ejecución: un build puede acceder a otros recursos del proceso.
- Tampoco proporciona una transacción crash-durable ni aislamiento de todos los metadatos Git.
- Los worktrees comparten metadatos y objetos del repositorio común.
- El árbol separado reduce interferencias de edición, pero no elimina riesgos de procesos externos.
- Usar rutas absolutas explícitas para cada operación y para invocar builds.
- No cambiar el directorio de trabajo global del servidor.
- La identidad y el estado deben revalidarse antes de cada operación sensible.

## 4. Base y precondiciones de begin
- La base será el commit resuelto desde `HEAD`, guardado como OID fijo e inmutable.
- Recomendar worktree detached para evitar movimientos implícitos de ramas.
- `HEAD` no captura cambios dirty, staged, untracked ni ignored del árbol original.
- En el MVP, rechazar inicialmente tracked dirty, incluidos cambios staged y bajas tracked.
- Untracked e ignored originales quedan excluidos y no se copian automáticamente.
- Comunicar estas exclusiones antes de devolver un workspace listo.
- Rechazar bases sin commit resoluble y verificar de nuevo las precondiciones ante concurrencia.
- No efectuar commits, stash, reset ni cambios implícitos del índice original.

## 5. Rutas, permisos y ámbito soportado
- Usar una raíz de destino dedicada, previamente autorizada y con identidad validada.
- La herramienta no amplía `allowed-paths` ni convierte una ruta solicitada en autorización.
- Validar origen, destino, ancestros y gitdir común contra el contrato de permisos aplicable.
- No usar como destino una root completa, el árbol principal ni un worktree ajeno.
- MVP limitado a repositorios locales y archivos regulares.
- Enlaces, junctions, SMB y submodules quedan sin soporte hasta disponer de pruebas específicas.
- Detectar esos casos y fallar explícitamente; no seguirlos ni degradar silenciosamente garantías.
- Controlar hooks, filtros y programas externos de Git antes de habilitar la creación del árbol.

## 6. Ownership persistente
- Mantener un registro propio fuera del árbol editable y protegido frente a escrituras ordinarias.
- Campos mínimos: `workspace_id`, `source_path`, `workspace_path` y `common_git_dir`.
- Añadir `base_commit`, `generation` y `lifecycle_state` al mismo registro.
- Guardar identidades canónicas verificables; una coincidencia textual de ruta no es suficiente.
- La sesión OCC y el audit log no constituyen prueba de ownership.
- No adoptar automáticamente worktrees descubiertos mediante Git o directorios preexistentes.
- Versionar el registro y definir cómo se detecta corrupción, pérdida o estado incompleto.
- La coordinación multicliente y el custodio del registro requieren decisión explícita en fase A.

## 7. Lifecycle y recuperación
- Estados propuestos: creating, ready, discarding, discarded y recovery_required.
- `begin` reserva identidad y destino antes de crear; publica ready tras verificar el worktree.
- Un fallo parcial conserva información suficiente para inspección y recuperación controlada.
- `status` expone estado registrado y observado, base fija, generación y divergencias.
- Serializar transiciones por workspace y comprobar generación para rechazar solicitudes obsoletas.
- Cancelación y reinicio no deben convertir un recurso incompleto en ready por inferencia.
- No prometer atomicidad entre el registro persistente y las operaciones internas de Git.
- Definir recuperación y retención antes de permitir limpieza automática de recursos incompletos.

## 8. Contrato mínimo de acciones
- `begin`: origen y destino autorizados; devuelve ID, rutas, base fija, estado y exclusiones.
- `status`: consulta por ID propio; reporta cambios, conflictos e identidad inconsistente.
- `diff`: revisión humana de los cambios del workspace respecto de su base fija.
- `export`: representación íntegra e identificable del conjunto de cambios seleccionado.
- `discard`: liberación explícita de un recurso propio, sujeta a precondiciones revalidadas.
- Especificar errores estables, condición retryable y resultados de cancelación por acción.
- No incluir consultas semánticas ni un motor `query` en este contrato.
- No aplicar cambios al árbol original como efecto lateral de ninguna acción del MVP.

## 9. Diff para revisión humana
- Paginar por archivos o unidades completas con orden estable y continuación explícita.
- Identificar workspace, base OID, generación y estado de captura en cada página.
- Invalidar la continuación si cambió el conjunto observado; no mezclar generaciones.
- Diferenciar ausente y vacío: una baja no equivale a un archivo de cero bytes.
- Mostrar altas seleccionadas, bajas y modificaciones; declarar el tratamiento de renombres.
- Hacer explícitos los binarios y los archivos untracked no seleccionados.
- Una vista truncada puede servir para revisión si se etiqueta; nunca equivale a un export íntegro.
- Desactivar diff externo y transformaciones no controladas que alteren el contenido revisado.

## 10. Export íntegro
- Acordar formato, manifiesto e identificación verificable del artefacto antes de implementarlo.
- Incluir base OID, selección de rutas, operaciones y hashes de contenido pertinentes.
- Representar explícitamente altas seleccionadas, bajas, vacíos y binarios, o rechazarlos claramente.
- No omitir archivos por límites de salida sin devolver un error de export incompleto.
- El límite actual de stdout Git de 1 MB exige una vía íntegra o un rechazo explícito.
- Nunca publicar como export válido una salida que el ejecutor Git haya truncado.
- Preferir devolver el artefacto y persistirlo mediante `write_file` en una operación separada.
- Si se admite export a disco, clasificarlo como mutación y validar destino, presupuesto y permisos.

## 11. Consistencia ante cambios concurrentes
- Fijar selección y precondiciones de contenido para cada diff o export.
- Capturar y comprobar hashes/identidades antes y después; fallar si la captura no es coherente.
- Una generación del registro no detecta por sí sola escrituras de herramientas externas.
- La comprobación hash seguida de rename mantiene una ventana TOCTOU; documentarla.
- No presentar lecturas sucesivas de varios archivos como snapshot atómico del sistema de archivos.
- Si no se garantiza coherencia suficiente, devolver conflicto y exigir nueva captura.
- Un ID de export debe identificar exactamente los bytes y operaciones efectivamente capturados.
- Definir cómo afecta un build concurrente que crea o modifica archivos dentro del workspace.

## 12. Discard conservador
- Sin force por defecto; rechazar eliminación con cambios pendientes o estado ambiguo.
- Operar exclusivamente sobre recursos propios acreditados por el registro y la identidad actual.
- Revalidar estado, generación, ubicación, gitdir e identidad inmediatamente antes de retirar.
- Nunca borrar una root autorizada, el árbol principal ni worktrees de otros usuarios o procesos.
- No sustituir un fallo de Git por un borrado recursivo genérico del destino.
- Detectar sustituciones de directorios, junctions y escapes de gitdir.
- Serializar doble discard; definir resultado idempotente sin adoptar un recurso recreado en la ruta.
- Cualquier descarte destructivo adicional exige un contrato explícito posterior, no un force implícito.

## 13. Integración transversal
- Con policy de archivos activa, bloquear operaciones Git según la política vigente.
- Aplicar readonly por acción: begin y discard mutan; export muta solo cuando escribe.
- Status, diff y export devuelto deben evitar refrescos o escrituras implícitas de Git.
- Integrar auditWrap con acción, ID, resultado, duración, bytes y conflictos relevantes.
- Aplicar mutation budget a cada acción mutante según el contrato acordado y probado.
- Redactar anotaciones honestas para una herramienta mixta y conservar controles por acción.
- Experimental sin outputSchema hasta graduación y cobertura del sweep correspondiente.
- Los 17 core están congelados: no introducir nuevas funciones allí sin decisión explícita.

## 14. Fases A y B: contrato y lifecycle
- A: resolver cambio de alcance del roadmap, formato export, ownership y ámbito soportado.
- A: fijar contrato de acciones, permisos, errores, exclusiones y criterios de aceptación.
- A: decidir ubicación del nuevo módulo de lifecycle y release de introducción experimental.
- B: implementar registro propio, begin, status y discard con identidades y generaciones.
- B: incorporar estados incompletos, cancelación, reinicio y recuperación verificable.
- B: controlar hooks/filtros y evitar modificaciones implícitas del repositorio original.
- Salida A: decisiones revisadas; salida B: lifecycle probado con fallos y recursos ajenos.
- No avanzar por asumir garantías de Git que todavía no estén cubiertas por pruebas.

## 15. Fases C, D y E
- C: implementar diff paginado y export íntegro con selección, binarios y concurrencia explícitos.
- C: verificar el caso superior a 1 MB y la distinción entre ausencia, vacío y truncamiento.
- D: integrar perfiles, validación, auditoría, permisos, presupuesto, help y documentación.
- D: ejecutar matriz funcional y builds sobre la ruta real del workspace.
- E: diseñar promote por separado como aplicación de changeset al working tree original.
- E: sin commits ni cambios implícitos del índice; exigir precondiciones y conflictos explícitos.
- E: hash más rename no elimina TOCTOU; varios archivos no tienen visibilidad atómica conjunta.
- E: rollback en proceso no equivale a durabilidad ante caída; no prometer transacción crash-durable.

## 16. Archivos previstos y pruebas
- Prever `internal/mcpserver/tools_workspace.go` y un módulo lifecycle nuevo, ubicación por decidir.
- Registro y auditWrap en `tools_core.go`; alta experimental en `experimental.go` con release acordada.
- Revisar `core/param_validator.go` y `core/tool_contract.go` para validación y clasificación mixta.
- Añadir pruebas de perfiles: ultra +1 si el resto sigue igual; agent/strict sin workspace.
- Actualizar docs/help y pruebas: permisos, readonly/policy, tracked/untracked/staged e aislamiento original.
- Cubrir export >1 MB, restart/cancel, junction/gitdir escape, concurrencia, doble discard y ownership.
- Prever builds por ruta real y `go test ./internal/mcpserver/ ./core/ -count=1`.
- Prever `go vet ./internal/mcpserver/ ./core/`; estos comandos NO se ejecutaron al crear el plan.

## 17. Aceptación y decisiones abiertas
- Aceptación: cero cambios implícitos en archivos o índice original en la matriz del MVP.
- Aceptación: cero borrados de recursos ajenos y cero exports truncados etiquetados como completos.
- Aceptación: 100 % de acciones mutantes cubiertas por permisos, auditoría y mutation budget.
- Aceptación: build visible desde ruta real y conflictos concurrentes detectados en los casos definidos.
- Medir p50/p95, bytes exportados, conflictos y recuperación; acordar umbrales antes del cierre de D.
- Abierto: formato export, selección de altas y tratamiento exacto de binarios/renombres.
- Abierto: ownership multicliente, bloqueo, retención, reinicio y recuperación tras fallo parcial.
- Abierto: política de hooks/filtros y evolución de enlaces, SMB y submodules tras pruebas específicas.

