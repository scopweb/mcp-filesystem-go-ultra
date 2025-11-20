# 📁 Estructura sugerida para tu proyecto:

proyecto/
├── .claude-rules                    # ← Versión COMPACTA (uso diario)
└── docs/
    └── filesystem-ultra-guide.md    # ← Versión COMPLETA (referencia)

    Añade esto en archivos claude-rules.

o en 

.claude/
├── rules/
│   └── filesystem-ultra.md    # Las reglas optimizadas
├── context.md                 # Contexto actual del proyecto (muevo de tu userMemories)
└── README.md                  # Índice navegable
y readme:
# Claude Configuration - JotaJotaPe CRM

## 📁 Estructura:
- `rules/` - Reglas específicas por herramienta/tecnología
- `context.md` - Contexto del proyecto y convenciones

## 📋 Reglas Activas:
1. [filesystem-ultra.md](rules/filesystem-ultra.md) - Optimización de tokens
2. [database-sqlserver.md](rules/database-sqlserver.md) - Queries seguros
3. [blazor-patterns.md](rules/blazor-patterns.md) - Patrones de código

## 🔄 Uso:
Estas reglas se aplican automáticamente cuando Claude trabaja en este proyecto.



y  filesystem-ultra :


# ⚡ FILESYSTEM-ULTRA: Token Optimization Rules

## 🚫 NUNCA:
❌ read_file() sin max_lines en archivos grandes
❌ Leer completo para editar 1 línea
❌ write_file() para reemplazar (usa recovery_edit)
❌ Operaciones individuales (usa batch_operations)

## ✅ SIEMPRE:

### 1️⃣ BUSCAR → LEER → EDITAR
smart_search(path, "patrón") → read_file_range(inicio, fin) → recovery_edit(old, new)

### 2️⃣ LECTURA PROGRESIVA
50 líneas → 100 líneas → 200 líneas → completo (último recurso)

### 3️⃣ TABLA DE DECISIÓN RÁPIDA

| Situación | Herramienta | Tokens |
|-----------|-------------|--------|
| Ver inicio | read_file(max_lines=50, mode="head") | ~500 |
| Buscar patrón | smart_search() | ~50 |
| Leer sección | read_file_range() | Variable |
| Cambiar texto | recovery_edit() | ~200 |
| Archivo >1000 líneas | read_file_range() OBLIGATORIO | - |
| Múltiples archivos | batch_operations() | Eficiente |

### 4️⃣ EJEMPLOS QUICK:

**Separar código:**
```
smart_search("@code {") → read_file_range(1,n) + read_file_range(n,-1) → batch_operations
AHORRO: 30% tokens vs leer completo
```

**Editar método:**
```
smart_search("MiMetodo") → read_file_range(inicio,fin) → recovery_edit
AHORRO: 95% tokens vs leer completo
```

## 🎯 META: >80% ediciones quirúrgicas
Check: get_edit_telemetry()

## 💰 AHORRO ESPERADO:
- <50KB: 10-20%
- 50-200KB: 40-60%
- >200KB: 70-90%

---
🧠 MANTRA: "Si lees más de lo que necesitas, estás desperdiciando tokens"
```

### 🚀 Cómo implementarlo ahora:

**Opción A) Solo Custom Instructions (más simple):**
1. Abre Claude Settings → Custom Instructions
2. Borra el texto actual de filesystem-ultra
3. Pega la versión compacta de arriba

**Opción B) Archivo en proyecto (más profesional):**
1. Crea `.claude-rules` en la raíz del proyecto
2. Pega la versión compacta
3. En Custom Instructions solo pon:
```
   Sigue las reglas en .claude-rules del proyecto actual
```

**Opción C) Híbrida (recomendada):**
1. Versión compacta en Custom Instructions
2. Versión completa (la primera que te di) en `docs/filesystem-ultra-guide.md`
3. Yo consulto la completa cuando tenga dudas

### 💡 Ventajas del nuevo:

1. **Más accionable**: Tabla de decisión → elijo herramienta directa
2. **Ejemplos reales**: Veo ahorro de tokens en casos reales
3. **Lectura progresiva**: Antes faltaba, ahora está claro
4. **Workflow completo**: No solo edición, todo el ciclo
5. **Visual**: Emojis + tablas = más fácil escanear
6. **Compacto**: 50 líneas vs 200 = carga más rápido en contexto

### ✨ Bonus: Mini-checklist mental

Cada vez que uses filesystem-ultra, pregúntate:
```
1. ¿Necesito leer TODO el archivo? → NO → usa read_file_range
2. ¿Sé dónde está lo que busco? → NO → usa smart_search primero
3. ¿Voy a hacer múltiples cambios? → SÍ → usa batch_operations
4. ¿El archivo es >1000 líneas? → SÍ → OBLIGATORIO read_file_range