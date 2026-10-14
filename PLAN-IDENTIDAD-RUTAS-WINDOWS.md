# Plan de reparación de la identidad de rutas en Windows

Fecha de investigación: 2026-10-11.

Repositorio: `C:\MCPs\clone\mcp-filesystem-go-ultra`.

**Estado: investigación realizada; reparación y regresiones pendientes.** Este documento no marca el plan anterior como terminado. Su creación no implica autorización para implementar el arreglo, hacer commit o push.

## 1. Objetivo y contexto

Reparar dos fallos observados en Windows cuando una ruta original y su ruta resuelta representan el mismo archivo con cadenas distintas:

- `TestBeginFileTxn_OCCConflictUsesMatchingBackup`: devuelve `Reason:no_baseline`.
- `TestReadFileRange_UsesCacheAfterWarm`: realiza una lectura de contenido desde disco con la caché caliente.

Separar tres conceptos:

1. **Ruta operativa:** ruta resuelta y autorizada, conservando su escritura para acceder al archivo.
2. **Identidad de comparación:** representación canónica para reconocer alias del mismo destino.
3. **Nombre persistido:** `OriginalPath` del backup y nombre del archivo en disco. No reescribirlos para solucionar una comparación.

La migración de `os.Root` en rutas de usuario es un antecedente ya realizado según el alcance indicado por el usuario. Esta tarea no debe convertirse en otra fase de migración.

### Estado comprobado durante la investigación

```text
HEAD        6fe4041572e1bd56ccae70e9bb61dfa3f7d90955
origin/main eba0cda91f33c5751a774ab43f993c18cd14fd57
Go          go1.27.2 windows/amd64
```

Había modificaciones previas en:

- `CHANGELOG.md`
- `HOJA-DE-RUTA.md`
- `core/file_uri_test.go`

También había archivos sin seguimiento, incluido `core/windows_parity_test.go`. No utilizarlos como dependencia necesaria del arreglo ni incluirlos en su eventual commit. Este documento nuevo también debe permanecer fuera de ese commit salvo autorización posterior explícita.

El handoff guardado describe un estado anterior de la migración. El código actual y las instrucciones del usuario prevalecen sobre ese handoff.

### Qué se investigó y qué queda por demostrar

Se inspeccionaron el código local, el estado de Git, el log de CI y fuentes oficiales de Go y Microsoft. Durante esa investigación no se modificó código ni se ejecutaron tests que creasen fixtures.

**No se realizó todavía la reproducción local forzada de caja y 8.3.** Es el primer paso de la futura implementación. El análisis confirma las comparaciones inconsistentes; falta registrar la reproducción controlada que cierre la demostración causal.

## 2. Evidencias del CI

Run conocido: <https://github.com/scopweb/mcp-filesystem-go-ultra/actions/runs/37925642178>.

El run corresponde a `0cec0657baad5912d8eff99d6ad4a5e51d5c15ec`, anterior al HEAD local. Sirve como evidencia histórica, no como validación de `6fe4041`.

El log contiene:

```text
--- FAIL: TestBeginFileTxn_OCCConflictUsesMatchingBackup
conflict={
  Path:C:\Users\runneradmin\AppData\Local\Temp\...\conflict.txt
  ChangedRanges:[]
  HunkCount:0
  BytesDelta:0
  Reason:no_baseline
}
```

```text
--- FAIL: TestReadFileRange_UsesCacheAfterWarm
ReadFileRange should not hit disk when cache is warm, got 1 reads
```

También aparecen rutas bajo `C:\Users\RUNNER~1\...` y rutas bajo `C:\Users\runneradmin\...`.

Los tests no imprimen todas las claves implicadas. La presencia de ambas representaciones y los fallos respaldan el diagnóstico, pero no sustituyen la reproducción dirigida.

El mismo run contiene fallos E2E en comparaciones de rutas mostradas. Registrarlos por separado: reparar estos dos tests no demuestra que todo aquel run quedaría verde. Además, el resumen del paso Windows de tests figura como exitoso aunque su log contiene fallos de `core`; para esta investigación se utilizó el log, no solo el resumen de pasos.

## 3. Causa identificada en el código

Las referencias de línea corresponden al estado inspeccionado; localizar las funciones por nombre si cambian.

### 3.1. Baseline OCC: dos filtros pueden descartar el backup

En `core/file_txn.go`, `BeginFileTxn`:

1. Normaliza la ruta.
2. Llama a `ResolveAndAuthorize`.
3. Sustituye la ruta por la resuelta cuando la resolución tiene éxito.
4. Detecta el conflicto de hash.
5. Llama a `FindOCCBaseline` con esa ruta.

En `FindOCCBaseline` hay dos filtros:

```go
ListBackups(0, "all", path, 0)
```

Y después:

```go
filepath.Clean(file.OriginalPath) != filepath.Clean(path)
```

`ListBackups`, en `core/backup_manager.go:334`, filtra mediante una búsqueda por subcadena pasando ambos textos a minúsculas.

| Diferencia | Filtro textual de ListBackups | Comparación final con Clean |
|---|---|---|
| Solo caja | Puede pasar | Puede fallar |
| Nombre largo frente a 8.3 | Puede descartar el backup | Puede fallar también |
| Alias mediante symlink | Puede descartar el backup | Puede fallar también |

**Cambiar solamente la comparación final a `EqualFold` no arregla 8.3.** El candidato puede haber sido descartado antes.

### 3.2. Caché: lectura completa y rango utilizan claves diferentes

- `core/engine.go:638`: `ReadFileSnapshot`, utilizado por `ReadFileContent`, resuelve y autoriza antes de consultar la caché.
- `core/read_dedup.go:22`: la lectura deduplicada usa la cadena recibida para `PeekFileFresh`, `singleflight`, generaciones y almacenamiento.
- `core/file_operations.go:889`: `ReadFileRange` aplica `NormalizePath`, pero no la misma resolución antes del lookup.
- `cache/intelligent.go:159`: `GetFileFresh` busca bytes y metadatos mediante claves textuales exactas.

```text
Lectura completa:
entrada -> resolución -> clave A -> guarda bytes y metadatos

Lectura por rango:
entrada -> NormalizePath -> clave B -> miss si B != A
```

El fallo ocurre antes de que la comprobación de frescura pueda aprovechar los metadatos del archivo correcto.

### 3.3. Helpers existentes que deben aprovecharse

- `ResolveAndAuthorize`, en `core/engine.go`: resolución y comprobación del destino permitido.
- `ResolveSymlinks`, en `core/path_security.go`: usa `filepath.EvalSymlinks` y resolución de ancestros para rutas ausentes.
- `CanonicalPath`, en `core/canonical.go`: ruta absoluta y resolución, con fallback para archivos ausentes.
- `CanonicalOCCKey`, en `core/canonical.go`: reutiliza `CanonicalPath` y pliega la caja en Windows para los mapas OCC.
- `invalidateFileReadCache`, `invalidateMutatedPath` y `forgetReadFlight`, en `core/read_dedup.go`.

No convertir estos helpers de identidad en sustitutos de la autorización ni de las garantías de `os.Root`.

## 4. Investigación externa y decisiones derivadas

### 4.1. Clean no identifica archivos

`filepath.Clean` hace limpieza léxica. No unifica por sí solo nombres largos, alias 8.3 y rutas con enlaces.

### 4.2. EvalSymlinks normaliza nombres en Windows

Se consultó el código de Go 1.27.2:

<https://raw.githubusercontent.com/golang/go/go1.27.2/src/path/filepath/symlink_windows.go>

La implementación normaliza la letra de unidad y recorre componentes usando `FindFirstFile` para recuperar el nombre con su caja real después de resolver enlaces.

**Matiz:** el alias 8.3 puede venir de la ruta temporal de entrada y `EvalSymlinks` expandirlo a `runneradmin`. No hace falta que `EvalSymlinks` produzca el nombre corto para que exista el bug.

### 4.3. Un alias 8.3 no está garantizado

Documentación oficial:

<https://learn.microsoft.com/en-us/windows/win32/api/fileapi/nf-fileapi-getshortpathnamew>

`GetShortPathNameW` puede devolver una ruta corta existente, pero también puede tener éxito devolviendo la ruta larga. Depende del sistema de archivos y de si existen nombres cortos.

Por tanto, el test debe comprobar que obtuvo un alias realmente distinto. No inventar un nombre `~1`, no asumir que una llamada exitosa prueba 8.3 y no activar globalmente nombres cortos para ejecutar el test.

### 4.4. Caja, nombre persistido e identidad física

Documentación oficial:

<https://learn.microsoft.com/en-us/windows/win32/fileio/naming-a-file>

Windows conserva los nombres y normalmente compara sin distinguir caja, aunque existen escenarios sensibles a ella.

Decisiones:

- No convertir rutas operativas o nombres persistidos a minúsculas.
- Reutilizar el contrato de identidad OCC existente sin ampliar este parche a un rediseño general de sensibilidad a caja.
- No usar el identificador físico NTFS como clave permanente: las escrituras mediante temporal y renombrado pueden reemplazar ese identificador.
- No modificar las pruebas existentes de ese comportamiento en `core/write_identity_windows_test.go` para hacerlas pasar por otra razón.

## 5. Plan de implementación paso a paso

### Paso 1 — Construir una reproducción que no pase por casualidad

Antes del cambio de producción, ampliar tests usando dos representaciones del mismo archivo.

Registrar en el diagnóstico del test:

```text
ruta usada para guardar el backup o calentar la caché
ruta usada para consultar
NormalizePath de ambas
resultado de ResolveAndAuthorize
identidad canónica de ambas
OriginalPath del backup
incremento de DiskLoads
```

Precondiciones obligatorias:

1. Ambas rutas existen.
2. `os.SameFile` confirma que apuntan al mismo archivo.
3. Las cadenas difieren incluso después de `filepath.Clean`.
4. Los controles de acceso aceptan el destino.
5. El fallo llega al baseline o a la caché, no a un error previo de apertura.

#### Windows: diferencia de caja

1. Crear un archivo de nombre mixto, por ejemplo `MixedCaseBaseline.txt`.
2. Construir una variante cambiando la caja del nombre.
3. Comprobar todas las precondiciones.
4. Probar A -> B y B -> A para backup/consulta y calentamiento/rango.

#### Windows: nombre 8.3

1. Crear un directorio de nombre largo dentro de `t.TempDir()`.
2. Obtener su alias con `GetShortPathNameW` usando un helper Windows.
3. Construir las rutas al mismo archivo mediante directorio largo y corto.
4. Comprobar que la diferencia no es solo de caja.
5. Probar ambas direcciones.

Si el volumen no ofrece un alias:

- Omitir solo el subtest 8.3 con una explicación precisa.
- Mantener obligatorio el caso de caja en un directorio Windows normal, insensible a caja.
- No declarar validado 8.3 hasta ejecutar el caso real en un entorno que lo proporcione.

#### Regresión portable para Linux

Crear un symlink dentro de la raíz permitida:

```text
root/real/archivo.txt
root/alias/archivo.txt -> root/real/archivo.txt
```

Esto fuerza cadenas distintas para el mismo destino sin depender de la semántica de caja de Windows.

La regresión portable debe fallar antes del arreglo y pasar después en Linux. No basta con saltarse todos los casos fuera de Windows. Si el entorno no permite symlinks, registrar la limitación y validar en otro entorno Linux que sí los permita.

### Paso 2 — Reparar FindOCCBaseline

Archivo principal: `core/file_txn.go`.

1. Mantener la autorización existente.
2. Obtener candidatos sin el filtro textual de ruta:

   ```go
   ListBackups(0, "all", "", 0)
   ```

3. Calcular una sola vez `CanonicalOCCKey(path)`.
4. Compararlo con `CanonicalOCCKey(file.OriginalPath)`.
5. Leer únicamente los backups candidatos por identidad.
6. Mantener la verificación del hash de sus bytes.

Pseudocódigo orientativo, no parche completo:

```go
wantedKey := CanonicalOCCKey(path)
for each backup file {
    if CanonicalOCCKey(file.OriginalPath) != wantedKey {
        continue
    }
    // Leer los bytes del backup y verificar el hash esperado como ahora.
}
```

Condiciones que deben conservarse:

- `OriginalPath` queda exactamente como se guardó.
- No se modifica el JSON de los backups.
- No se cambia el filtro público de `ListBackups`: sus otros consumidores siguen teniendo un filtro textual.
- Un backup de otro archivo con los mismos bytes no es válido.
- Un backup del archivo correcto con hash distinto no es válido.
- No sustituir la comprobación actual por `BackupMetadata.Hash`: el backup almacena SHA-256, mientras que OCC utiliza su hash de contenido actual.

Criterio de aceptación: sigue produciéndose `OCCMismatchError`, pero se recupera el baseline y el informe devuelve el cambio de la línea 2, sin `Reason:no_baseline`.

### Paso 3 — Unificar las rutas de lectura de caché

Archivo principal: `core/file_operations.go`, función `ReadFileRange`.

Hacer que siga el mismo procedimiento de resolución que `ReadFileSnapshot` antes de consultar caché:

```text
normalizar entrada
validar acceso y política de lectura
resolver con ResolveAndAuthorize
validar política sobre el destino resuelto
consultar y cargar usando esa ruta resuelta
```

Mantener separada la ruta de presentación si es necesario conservar el nombre mostrado en el pie del rango.

La misma ruta resuelta debe llegar a:

- `GetFileFresh`
- `readFileBytesDeduped`
- `PeekFileFresh`
- `CaptureGeneration`
- `SetFileIfGeneration`
- La clave de `singleflight`

Empezar por este cambio localizado. `GetFileFresh` funciona cuando recibe la clave utilizada al almacenar; no necesita importar `core` ni aprender una segunda política de resolución.

La identidad compartida procede de resolver ambos alias al mismo destino. El plegado de caja de OCC es una operación de comparación, no una ruta para escribir archivos.

#### Comprobar coherencia adyacente

Revisar:

- `ReadSnapshot`, en `core/file_txn.go`.
- La entrada de `readFileBytesDeduped`.
- `InvalidateCache`.
- `invalidateFileReadCache` y `forgetReadFlight`.

Añadir una prueba que caliente con A e invalide con B. Si falla, ajustar de manera localizada la normalización de claves de estos puntos.

No corregir solo el lookup dejando almacenamiento, generaciones o invalidación con otra identidad. No introducir una tabla permanente de alias: quedaría obsoleta con renombrados o cambios de symlinks.

Para invalidaciones tras la desaparición del archivo, conservar la eliminación mediante la clave recibida y no hacer que una resolución fallida impida invalidar. Cubrir explícitamente ese comportamiento antes de ampliar el helper compartido.

### Paso 4 — Añadir regresiones en archivos controlados

Para respetar la exclusión de archivos sin seguimiento, ampliar preferentemente los existentes:

| Archivo | Cobertura propuesta |
|---|---|
| `core/occ_conflict_test.go` | Baseline con alias y negativos de ruta/hash |
| `core/read_dedup_test.go` | Caché caliente, alias e invalidación |
| `core/write_identity_windows_test.go` | Helpers y casos de caja/8.3, aprovechando su build tag |
| `core/file_txn_test.go` | Contrato de identidad, si es necesario |

Los helpers que importen APIs exclusivas de Windows deben permanecer en un archivo con build tag Windows. No referenciarlos desde tests comunes que deban compilar en Linux; situar allí los casos Windows completos o proporcionar separación de plataforma correcta.

No modificar las pruebas existentes de identidad NTFS ni incorporar `core/windows_parity_test.go`.

#### Aserciones OCC

- Rutas de entrada distintas.
- `OriginalPath` conserva el valor original.
- Baseline correcto recuperado.
- `Reason == ""`.
- Rango cambiado: línea 2.
- Otro archivo con contenido idéntico no se acepta.
- Hash no coincidente no se acepta.

#### Aserciones de caché

- Calentar con lectura completa.
- Leer el rango mediante el alias.
- Comprobar contenido y pie del rango.
- Incremento de cargas de contenido desde disco: cero.
- Repetir invirtiendo los alias.

Medir cargas de contenido, no exigir cero consultas de metadatos: resolver rutas y comprobar frescura puede requerir operaciones del sistema de archivos.

#### Aserciones de coherencia

- Una modificación externa sigue detectándose.
- Invalidar mediante otro alias impide reutilizar contenido invalidado.
- Una captura en vuelo no repuebla una generación invalidada.
- En Linux, dos archivos distintos `A.txt` y `a.txt` no se confunden.
- Una escritura no cambia la caja del nombre del archivo en disco.

Evitar `t.Parallel()` en tests que utilizan `diskReadCount` o hooks globales de captura. Aislar los backups con directorios temporales para que el resultado no dependa de backups de ejecuciones anteriores.

### Paso 5 — Validar rojo antes y verde después

Antes del cambio de producción, ejecutar las nuevas regresiones y conservar la evidencia de fallo:

- Diferencia de caja.
- Alias 8.3 realmente ejecutado.
- Alias portable mediante symlink.

No aceptar como reproducción un fallo de «ruta fuera de la raíz» si se pretende demostrar un miss de caché.

Después del arreglo, ejecutar los dos tests originales:

```powershell
go test ./core -run '^(TestBeginFileTxn_OCCConflictUsesMatchingBackup|TestReadFileRange_UsesCacheAfterWarm)$' -count=1 -v
```

Ejecutar las nuevas regresiones por sus nombres concretos. Si sus nombres incluyen `PathIdentity`, repetir el grupo dirigido:

```powershell
go test ./core -run 'PathIdentity|OCCConflictUsesMatchingBackup|UsesCacheAfterWarm' -count=20
```

Confirmar en la salida que el patrón selecciona los tests nuevos; un comando exitoso sin ejecutar los casos esperados no cuenta como validación.

Después:

```powershell
go test ./cache ./core ./internal/mcpserver ./tests/security -count=1
```

En Linux real:

```bash
go test ./cache ./core ./internal/mcpserver ./tests/security -count=1
go test -race ./cache ./core -count=1
```

Una compilación cruzada no valida la semántica del sistema de archivos Linux. Registrar cualquier subtest omitido y distinguir los resultados locales de los del runner. No atribuir al parche fallos previos ajenos al alcance.

### Paso 6 — Revisar alcance y eventual commit

El diff esperado debe concentrarse en:

- `core/file_txn.go`
- `core/file_operations.go`
- Helpers de lectura e invalidación estrictamente necesarios.
- Tests existentes relacionados.

Antes de un eventual commit autorizado:

1. Revisar `git status`, diff completo e historial reciente.
2. Seleccionar archivos explícitamente; no usar `git add .` ni `git add -A`.
3. Excluir `CHANGELOG.md`, `HOJA-DE-RUTA.md`, `core/file_uri_test.go` y todos los archivos sin seguimiento.
4. No modificar ni dar por terminado el plan anterior.
5. No hacer push.

## 6. Criterios de aceptación final

- [ ] La regresión demuestra cadenas distintas para el mismo archivo.
- [ ] Hay evidencia de fallo antes del arreglo en los puntos previstos.
- [ ] El baseline se encuentra por identidad y hash, no por semejanza textual.
- [ ] `OriginalPath` no se modifica.
- [ ] El caso Windows de caja pasa.
- [ ] El caso 8.3 se ha ejecutado realmente y pasa; si está omitido, queda explícitamente pendiente.
- [ ] El caso portable pasa en Linux.
- [ ] Caché caliente: cero nuevas cargas de contenido al leer rangos mediante alias.
- [ ] La invalidación y las generaciones mantienen la misma identidad.
- [ ] La frescura ante cambios externos sigue funcionando.
- [ ] No se altera el nombre persistido en disco.
- [ ] Se respetan los archivos y subsistemas excluidos.
- [ ] El plan anterior permanece intacto y no hay push.

## 7. Fuera de alcance

- Soft-delete.
- Copia entre raíces.
- Restauración de backups.
- Implementación de git.
- Hooks.
- `lint`.
- Nuevas fases de migración a `os.Root`.
- Rediseño global de caché, locks, backups o identidad NTFS.
- Investigación de fugas de handles.

Antecedente proporcionado por el usuario: 200 lecturas y escrituras dejan un solo `os.Root` y `Close` lo libera. El arranque retiene aproximadamente 60 handles de hilos y caché. No reinterpretar esta tarea como una caza de fugas ni presentar esa medición como realizada durante esta investigación.

## 8. Fuentes

1. Run de CI: <https://github.com/scopweb/mcp-filesystem-go-ultra/actions/runs/37925642178>
2. Código Go 1.27.2, resolución Windows: <https://raw.githubusercontent.com/golang/go/go1.27.2/src/path/filepath/symlink_windows.go>
3. Código de referencia Go: <https://go.dev/src/path/filepath/symlink_windows.go>
4. Microsoft, GetShortPathNameW: <https://learn.microsoft.com/en-us/windows/win32/api/fileapi/nf-fileapi-getshortpathnamew>
5. Microsoft, nombres y namespaces: <https://learn.microsoft.com/en-us/windows/win32/fileio/naming-a-file>
6. Código local inspeccionado: `core/file_txn.go`, `core/canonical.go`, `core/engine.go`, `core/file_operations.go`, `core/read_dedup.go`, `core/backup_manager.go`, `core/path_security.go`, `core/osroot.go`, `cache/intelligent.go`, `cache/capture.go` y tests asociados.

## 9. Plan de ataque corto

1. **Causa:** se mezclan rutas originales y resueltas como cadenas exactas. OCC añade un prefiltro textual que pierde los alias 8.3.
2. **Reproducir:** forzar caja distinta y alias 8.3 real en Windows; usar un symlink para una regresión ejecutable en Linux.
3. **Arreglar OCC:** eliminar el prefiltro textual solo en `FindOCCBaseline`, comparar mediante identidad canónica y conservar la comprobación del hash.
4. **Arreglar caché:** resolver y autorizar `ReadFileRange` antes del lookup, utilizando la misma ruta que la lectura completa; comprobar almacenamiento, deduplicación e invalidación.
5. **Validar:** rojo antes/verde después, cero nuevas cargas de contenido con caché caliente, baseline correcto, nombres persistidos intactos y Linux sin colisiones de caja.
6. **Límite:** parche localizado, exclusiones respetadas, plan antiguo intacto y ningún push.
