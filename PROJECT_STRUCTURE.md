# Estructura del Proyecto MCP Filesystem Ultra-Fast v3.0

## 📁 Estructura Organizada

```
mcp-filesystem-go-ultra/
│
├── 📄 README.md                    # Documentación principal
├── 📄 CHANGELOG.md                 # Historia de cambios y versiones
├── 📄 LICENSE                      # Licencia del proyecto
├── 📄 go.mod                       # Dependencias de Go
├── 📄 go.sum                       # Checksums de dependencias
├── 📄 main.go                      # Punto de entrada del servidor
├── 📄 .gitignore                   # Archivos ignorados por Git
├── 📄 .gitattributes               # Atributos de Git
├── 🔧 mcp-filesystem-ultra.exe     # Ejecutable compilado
│
├── 📂 core/                        # Código fuente principal
│   ├── engine.go                   # Motor principal del servidor
│   ├── file_operations.go          # Operaciones de archivos básicas
│   ├── edit_operations.go          # Operaciones de edición
│   ├── streaming_operations.go     # Operaciones streaming para archivos grandes
│   ├── claude_optimizer.go         # Optimizaciones para Claude Desktop
│   ├── batch_operations.go         # Sistema de batch operations (v2.6)
│   ├── plan_mode.go                # Análisis dry-run (v2.5)
│   ├── hooks.go                    # Sistema de hooks (v2.4)
│   └── engine_test.go              # Tests del engine
│
├── 📂 cache/                       # Sistema de caché
│   └── intelligent_cache.go        # Implementación de caché inteligente
│
├── 📂 mcp/                         # Tipos y estructuras MCP
│   └── types.go                    # Definiciones de tipos MCP
│
├── 📂 docs/                        # 📚 Documentación Técnica
│   ├── README.md                   # Índice de documentación
│   ├── TOKEN_ANALYSIS.md           # Análisis de uso de tokens
│   ├── PHASE3_TOKEN_OPTIMIZATIONS.md  # Optimizaciones Fase 3
│   ├── TOKEN_OPTIMIZATION_SUMMARY.md  # Resumen de optimizaciones
│   ├── benchmarks.md               # Resultados de benchmarks
│   ├── RESULTADO_FINAL.md          # Resumen final del proyecto
│   └── PasosSiguientesParaSubirAHuggingFace.txt
│
├── 📂 guides/                      # 📖 Guías de Usuario
│   ├── README.md                   # Índice de guías
│   ├── CLAUDE_DESKTOP_SETUP.md     # Setup de Claude Desktop
│   ├── Claude_Desktop_Performance_Guide.md  # Guía de rendimiento
│   ├── CLAUDE_DESKTOP_MEMORY_INSTRUCTIONS.md  # Instrucciones memoria (completa)
│   ├── CLAUDE_MEMORY_SHORT.txt     # ⭐ Memoria Claude (copiar aquí)
│   ├── BATCH_OPERATIONS_GUIDE.md   # Guía de batch operations
│   ├── HOOKS.md                    # Guía del sistema de hooks
│   ├── Config_Claude_Desktop.md    # Configuración básica
│   └── Config_Claude_Desktop_Fixed.md
│
├── 📂 examples/                    # 📝 Ejemplos y Plantillas
│   ├── README.md                   # Índice de ejemplos
│   ├── hooks.example.json          # Ejemplo de configuración de hooks
│   ├── request.json                # Ejemplos de requests MCP
│   └── test_request.json           # Requests de prueba
│
├── 📂 tests/                       # 🧪 Tests
│   ├── README.md                   # Índice de tests
│   ├── mcp_functions_test.go       # Tests unitarios
│   └── test_new_features.md        # Documentación de tests
│
└── 📂 scripts/                     # 🔨 Scripts de Utilidad
    ├── README.md                   # Índice de scripts
    └── build.bat                   # Script de compilación Windows
```

## 📋 Archivos en Raíz (Solo Esenciales)

| Archivo | Descripción |
|---------|-------------|
| **README.md** | Documentación principal del proyecto |
| **CHANGELOG.md** | Historia completa de cambios y versiones |
| **LICENSE** | Licencia MIT del proyecto |
| **main.go** | Código fuente principal del servidor |
| **go.mod / go.sum** | Gestión de dependencias de Go |
| **mcp-filesystem-ultra.exe** | Ejecutable compilado listo para usar |
| **.gitignore / .gitattributes** | Configuración de Git |

## 📚 Guía Rápida de Navegación

### Para Usuarios Nuevos
1. 📖 Empieza con [README.md](README.md)
2. ⚙️ Configura siguiendo [guides/CLAUDE_DESKTOP_SETUP.md](guides/CLAUDE_DESKTOP_SETUP.md)
3. 💾 Copia [guides/CLAUDE_MEMORY_SHORT.txt](guides/CLAUDE_MEMORY_SHORT.txt) en Claude Desktop

### Para Usar Características Avanzadas
- 📦 Batch Operations: [guides/BATCH_OPERATIONS_GUIDE.md](guides/BATCH_OPERATIONS_GUIDE.md)
- 🪝 Hooks (auto-format): [guides/HOOKS.md](guides/HOOKS.md)
- 🎯 Optimización de tokens: [docs/PHASE3_TOKEN_OPTIMIZATIONS.md](docs/PHASE3_TOKEN_OPTIMIZATIONS.md)

### Para Desarrolladores
- 🔍 Análisis técnico: [docs/TOKEN_ANALYSIS.md](docs/TOKEN_ANALYSIS.md)
- 📊 Benchmarks: [docs/benchmarks.md](docs/benchmarks.md)
- 🧪 Tests: `go test ./...`

## 🎯 Archivos Más Importantes

### Top 5 Para Usuarios
1. **README.md** - Empieza aquí
2. **guides/CLAUDE_MEMORY_SHORT.txt** - Copia en Claude Desktop
3. **guides/CLAUDE_DESKTOP_SETUP.md** - Configuración
4. **guides/BATCH_OPERATIONS_GUIDE.md** - Operaciones avanzadas
5. **CHANGELOG.md** - Qué hay de nuevo

### Top 5 Para Desarrolladores
1. **main.go** - Código principal
2. **core/engine.go** - Motor del servidor
3. **core/batch_operations.go** - Sistema de batch
4. **docs/TOKEN_ANALYSIS.md** - Análisis técnico
5. **tests/** - Suite de tests

## 🚀 Compilación y Ejecución

### Compilar
```bash
go build -o mcp-filesystem-ultra.exe
```

O usa el script:
```bash
scripts\build.bat
```

### Ejecutar
```bash
mcp-filesystem-ultra.exe --compact-mode --max-search-results=20
```

### Ejecutar Tests
```bash
go test ./...
```

## 📊 Estadísticas del Proyecto

- **Archivos de código Go**: 15+
- **Herramientas MCP**: 32
- **Tests**: 16
- **Documentación**: 20+ archivos
- **Líneas de código**: ~10,000+
- **Tamaño ejecutable**: 7.9 MB

## 🎉 Versión Actual: 3.0.0

**Ultra Token Optimization** - 77% reducción de tokens

---

**Proyecto completo y listo para producción** ✅
