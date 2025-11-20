# Plan de Mejoras de Rendimiento (I/O y WSL)

Este documento detalla las áreas de mejora identificadas para optimizar la velocidad de lectura y escritura, especialmente considerando el entorno WSL y el uso por agentes de IA.

## 🔍 Diagnóstico Actual

1.  **Uso Excesivo de Memoria en Copias**:
    *   Funciones como `CopyFile`, `copyDirectoryRecursive` y `SyncWorkspace` utilizan `os.ReadFile` seguido de `os.WriteFile`.
    *   **Problema**: Carga el archivo completo en RAM antes de escribirlo. Para archivos grandes (>100MB), esto es lento y consume mucha memoria.
    *   **Impacto**: Latencia alta y posible OOM (Out of Memory) en operaciones masivas.

2.  **Lectura de Rangos Ineficiente**:
    *   `ReadFileRange` lee el archivo **completo** en memoria (`os.ReadFile`) y luego extrae las líneas solicitadas.
    *   **Problema**: Derrota el propósito de leer solo un rango. Leer un archivo de 1GB para obtener las líneas 10-20 es extremadamente ineficiente.

3.  **Falso `mmap` en Windows**:
    *   El archivo `core/mmap.go` tiene un fallback para Windows que usa `file.ReadAt` en lugar de mapeo de memoria real.
    *   **Problema**: Se pierde la ventaja de velocidad del acceso directo a memoria del kernel en Windows nativo.

4.  **Streaming Simulado**:
    *   `ChunkedReadFile` lee en trozos pero concatena todo en un `strings.Builder` en memoria antes de retornar.
    *   **Problema**: No es verdadero streaming si el resultado final se construye en RAM.

## 🚀 Plan de Optimización

### Fase 1: Optimización de I/O Básico (Alta Prioridad)

1.  **Implementar `io.Copy` para Copias**:
    *   Reemplazar `os.ReadFile` + `os.WriteFile` por `io.Copy` (o `io.CopyBuffer`) en todas las operaciones de copia y sincronización.
    *   **Beneficio**: Uso de memoria constante (ej. 32KB buffer) independientemente del tamaño del archivo. Aprovecha optimizaciones del sistema operativo (como `sendfile` en Linux/WSL).

2.  **Optimizar `ReadFileRange`**:
    *   Usar `bufio.Scanner` o `bufio.Reader` para leer línea por línea sin cargar todo el archivo.
    *   Para rangos muy avanzados en archivos grandes, investigar si se puede estimar el offset (aunque es difícil con líneas de longitud variable).
    *   **Beneficio**: Reducción drástica de latencia y memoria para leer fragmentos de logs o archivos grandes.

### Fase 2: Optimización de Memoria y Buffers

3.  **Implementar `sync.Pool` para Buffers**:
    *   Crear un pool global de buffers de bytes para reutilizar memoria en operaciones de lectura/escritura.
    *   **Beneficio**: Reducción de la presión sobre el Garbage Collector (GC) de Go.

4.  **Mejorar `mmap` en Windows**:
    *   Implementar mapeo de memoria real usando `syscall` o `golang.org/x/sys/windows` para Windows.
    *   **Beneficio**: Lecturas ultrarrápidas en Windows nativo, similar a Linux/WSL.

### Fase 3: Optimizaciones Específicas para WSL

5.  **Detección de Fronteras WSL/Windows**:
    *   Detectar si una operación cruza el sistema de archivos (ej. de `/mnt/c` a `/home`).
    *   Ajustar el tamaño del buffer: El sistema de archivos cruzado (Plan 9 / DrvFs) suele beneficiarse de buffers más grandes (ej. 1MB vs 32KB) para reducir el número de llamadas al sistema (syscalls), que son costosas entre WSL y Windows.

6.  **Paralelismo Inteligente**:
    *   En operaciones por lotes (`batch_operations`), ajustar la concurrencia basándose en si es I/O local o cruzado. El I/O cruzado puede saturarse antes.

## 📅 Pasos Siguientes

1.  Crear una rama de optimización.
2.  Refactorizar `core/file_operations.go` para usar `io.Copy`.
3.  Reescribir `ReadFileRange` en `core/file_operations.go`.
4.  Implementar `sync.Pool` en `core/engine.go`.
5.  Investigar implementación segura de `mmap` para Windows.

---
*Este plan está diseñado para maximizar el rendimiento sin cambiar la API externa del servidor MCP.*
