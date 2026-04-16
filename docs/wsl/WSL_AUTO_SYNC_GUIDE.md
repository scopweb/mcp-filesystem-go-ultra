# 🔄 Guía Completa: WSL Auto-Sync

> **Sincronización Automática y Silenciosa entre WSL y Windows**  
> Versión: 3.4.0 | Última actualización: 2025-11-15

---

## 📋 **TABLA DE CONTENIDOS**

1. [¿Qué es WSL Auto-Sync?](#qué-es-wsl-auto-sync)
2. [Instalación y Configuración](#instalación-y-configuración)
3. [Métodos de Activación](#métodos-de-activación)
4. [Opciones Avanzadas](#opciones-avanzadas)
5. [Casos de Uso](#casos-de-uso)
6. [Troubleshooting](#troubleshooting)
7. [FAQ](#faq)

---

## 🎯 **¿QUÉ ES WSL AUTO-SYNC?**

### **Problema que Resuelve**

**ANTES** (sin auto-sync):
```bash
# 1. Editar archivo en WSL
vim /home/user/app.go

# 2. Copiar manualmente a Windows
cp /home/user/app.go /mnt/c/Users/user/app.go

# 3. Repetir para CADA cambio... 😤
```

**AHORA** (con auto-sync):
```bash
# 1. Editar archivo en WSL
vim /home/user/app.go

# 2. ✨ ¡Se copia automáticamente a Windows!
# → Sin intervención manual
# → Sin scripts adicionales
# → Transparente y silencioso
```

### **Características Principales**

✅ **Automático**: Sincroniza al escribir/editar archivos  
✅ **Silencioso**: Sin logs molestos (modo configurable)  
✅ **Asíncrono**: No bloquea tus operaciones  
✅ **Inteligente**: Solo sincroniza lo necesario  
✅ **Configurable**: Filtros, exclusiones, mappings  

---

## 🚀 **INSTALACIÓN Y CONFIGURACIÓN**

### **Pre-requisitos**

1. ✅ Windows 10/11 con WSL2
2. ✅ MCP Filesystem Ultra v3.4.0+
3. ✅ Ejecutar MCP desde **dentro de WSL**

**Verificar WSL**:
```bash
cat /proc/version
# Debe contener "microsoft" o "WSL"

echo $WSL_DISTRO_NAME
# Debe mostrar: Ubuntu, Debian, etc.
```

### **Instalación del MCP**

```bash
# 1. Clonar repositorio
cd ~
git clone https://github.com/scopweb/mcp-filesystem-go-ultra.git
cd mcp-filesystem-go-ultra

# 2. Compilar
go build -o filesystem-ultra

# 3. Mover a PATH (opcional)
sudo mv filesystem-ultra /usr/local/bin/
```

---

## 🔧 **MÉTODOS DE ACTIVACIÓN**

### **MÉTODO 1: Variable de Entorno** ⭐ (Más Rápido)

**Temporal** (solo sesión actual):
```bash
export MCP_WSL_AUTOSYNC=true
filesystem-ultra
```

**Permanente** (agregar a `~/.bashrc` o `~/.zshrc`):
```bash
echo 'export MCP_WSL_AUTOSYNC=true' >> ~/.bashrc
source ~/.bashrc
```

**Verificar**:
```bash
echo $MCP_WSL_AUTOSYNC
# Output: true
```

---

### **MÉTODO 2: Archivo de Configuración** ⭐ (Más Control)

**Ubicación**: `~/.config/mcp-filesystem-ultra/autosync.json`

**Configuración Básica**:
```bash
mkdir -p ~/.config/mcp-filesystem-ultra
cat > ~/.config/mcp-filesystem-ultra/autosync.json << 'EOF'
{
  "wsl_auto_sync": {
    "enabled": true,
    "sync_on_write": true,
    "sync_on_edit": true,
    "sync_on_delete": false,
    "silent": false,
    "config_version": "1.0"
  }
}
EOF
```

**Configuración Avanzada** (con filtros):
```json
{
  "wsl_auto_sync": {
    "enabled": true,
    "sync_on_write": true,
    "sync_on_edit": true,
    "sync_on_delete": false,
    "silent": true,
    "exclude_patterns": [
      "*.tmp",
      "*.swp",
      "*.log",
      "node_modules/*",
      ".git/*",
      "__pycache__/*"
    ],
    "only_subdirs": [
      "/home/user/projects",
      "/home/user/documents"
    ],
    "target_mapping": {
      "/home/user/special/file.txt": "D:\\CustomLocation\\file.txt"
    },
    "config_version": "1.0"
  }
}
```

**Iniciar MCP**:
```bash
filesystem-ultra
# Log esperado: "🔄 WSL auto-sync enabled"
```

---

### **MÉTODO 3: Herramienta MCP** ⭐ (Desde Claude Desktop)

**Desde Claude Desktop**, usar la herramienta MCP:

```json
{
  "tool": "configure_autosync",
  "arguments": {
    "enabled": true,
    "sync_on_write": true,
    "sync_on_edit": true,
    "silent": true
  }
}
```

**Respuesta esperada**:
```
✅ Auto-sync enabled!

Files written/edited in WSL will be automatically copied to Windows.
You can disable it anytime with: configure_autosync --enabled false
```

---

## ⚙️ **OPCIONES AVANZADAS**

### **Opciones de Configuración**

| Opción | Tipo | Default | Descripción |
|--------|------|---------|-------------|
| `enabled` | bool | `false` | Activar/desactivar auto-sync |
| `sync_on_write` | bool | `true` | Sincronizar al escribir archivos |
| `sync_on_edit` | bool | `true` | Sincronizar al editar archivos |
| `sync_on_delete` | bool | `false` | Borrar en Windows al borrar en WSL |
| `silent` | bool | `false` | No mostrar logs de sincronización |
| `exclude_patterns` | []string | `[]` | Patrones de archivos a excluir |
| `only_subdirs` | []string | `[]` | Solo sincronizar estos directorios |
| `target_mapping` | map | `{}` | Mapeo custom de rutas |

---

### **Filtros de Exclusión**

**Sintaxis**: Glob patterns estándar

**Ejemplos**:
```json
{
  "exclude_patterns": [
    "*.tmp",           // Todos los .tmp
    "*.swp",           // Archivos swap de Vim
    "*.log",           // Logs
    "node_modules/*",  // Todo node_modules
    ".git/*",          // Todo .git
    "__pycache__/*",   // Python cache
    "*.pyc",           // Python compiled
    ".env",            // Variables de entorno
    "secrets.json"     // Archivos sensibles
  ]
}
```

**Caso de Uso**:
```bash
# Estos archivos NO se sincronizan:
echo "temp" > /home/user/test.tmp       # ❌ Excluido
echo "data" > /home/user/node_modules/x # ❌ Excluido

# Estos archivos SÍ se sincronizan:
echo "code" > /home/user/app.go         # ✅ Sincronizado
echo "data" > /home/user/data.json      # ✅ Sincronizado
```

---

### **Sincronización Selectiva de Directorios**

**Solo sincronizar directorios específicos**:

```json
{
  "only_subdirs": [
    "/home/user/projects",
    "/home/user/documents",
    "/home/user/Desktop"
  ]
}
```

**Comportamiento**:
```bash
# Dentro de subdirs permitidos → SINCRONIZA
echo "sync" > /home/user/projects/app.go    # ✅
echo "sync" > /home/user/documents/doc.txt  # ✅

# Fuera de subdirs permitidos → NO SINCRONIZA
echo "ignore" > /home/user/temp/test.txt    # ❌
echo "ignore" > /home/user/Downloads/file   # ❌
```

---

### **Mapeo Custom de Rutas**

**Redirigir archivos específicos a ubicaciones custom**:

```json
{
  "target_mapping": {
    "/home/user/config.json": "D:\\AppConfig\\config.json",
    "/home/user/logs/app.log": "C:\\Logs\\myapp.log"
  }
}
```

**Comportamiento**:
```bash
# Sin mapping:
echo "data" > /home/user/config.json
# → Copia a: C:\Users\user\config.json

# Con mapping:
echo "data" > /home/user/config.json
# → Copia a: D:\AppConfig\config.json (custom)
```

---

## 💡 **CASOS DE USO**

### **CASO 1: Desarrollo Web en WSL + VSCode en Windows**

**Escenario**: Trabajas con Node.js en WSL pero usas VSCode en Windows.

**Setup**:
```bash
# 1. Habilitar auto-sync
export MCP_WSL_AUTOSYNC=true

# 2. Configurar exclusiones
cat > ~/.config/mcp-filesystem-ultra/autosync.json << 'EOF'
{
  "wsl_auto_sync": {
    "enabled": true,
    "silent": true,
    "exclude_patterns": ["node_modules/*", "*.log", ".git/*"]
  }
}
EOF

# 3. Crear proyecto
mkdir /home/user/projects/webapp
cd /home/user/projects/webapp
npm init -y
```

**Workflow**:
```bash
# 4. Claude Desktop edita index.js en WSL
# → Auto-sync copia a C:\Users\user\projects\webapp\index.js
# → VSCode detecta cambio y actualiza
# → Sin intervención manual ✨
```

---

### **CASO 2: Scripts de Backup**

**Escenario**: Generas backups en WSL que deben estar en Windows.

**Setup**:
```json
{
  "wsl_auto_sync": {
    "enabled": true,
    "only_subdirs": ["/home/user/backups"],
    "target_mapping": {
      "/home/user/backups": "D:\\Backups"
    }
  }
}
```

**Script**:
```bash
#!/bin/bash
# backup.sh

DATE=$(date +%Y%m%d)
tar -czf /home/user/backups/backup_$DATE.tar.gz /home/user/data

# Auto-sync copia a D:\Backups\backup_20251115.tar.gz
# → Disponible en Windows inmediatamente
```

---

### **CASO 3: Logs en Tiempo Real**

**Escenario**: Aplicación genera logs en WSL, analista lee en Windows.

**Setup**:
```json
{
  "wsl_auto_sync": {
    "enabled": true,
    "only_subdirs": ["/home/user/logs"],
    "silent": true
  }
}
```

**App**:
```bash
# App escribe logs continuamente
while true; do
    echo "$(date): Request processed" >> /home/user/logs/app.log
    sleep 1
done

# Windows puede leer en tiempo real:
# tail -f /mnt/c/Users/user/logs/app.log
```

---

## 🔍 **VERIFICACIÓN Y MONITOREO**

### **Verificar Estado**

**Desde Claude Desktop** (herramienta MCP):
```json
{
  "tool": "autosync_status"
}
```

**Respuesta**:
```
🔄 Auto-Sync Status
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
Status: ✅ ENABLED
Environment: WSL
Windows User: <your-username>

⚙️  Configuration:
  Sync on Write: true
  Sync on Edit: true
  Sync on Delete: false

📄 Config File: /home/<your-username>/.config/mcp-filesystem-ultra/autosync.json
```

### **Ver Logs de Sincronización**

**Si `silent: false`**:
```bash
[AutoSync] Synced: /home/user/test.txt -> C:\Users\user\test.txt
[AutoSync] Synced: /home/user/app.go -> C:\Users\user\app.go
```

**Si `silent: true`**:
```bash
# Sin logs (silencioso)
```

---

## 🐛 **TROUBLESHOOTING**

### **Problema 1: Auto-sync no funciona**

**Síntomas**: Archivos no se copian a Windows

**Diagnóstico**:
```bash
# 1. Verificar que estás en WSL
cat /proc/version | grep -i microsoft
# Debe contener "microsoft" o "WSL"

# 2. Verificar variable de entorno
echo $MCP_WSL_AUTOSYNC
# Debe ser: true

# 3. Ver estado
# (desde Claude Desktop)
autosync_status
```

**Solución**:
```bash
# Si no está en WSL → No funcionará (solo funciona en WSL)

# Si variable no está configurada:
export MCP_WSL_AUTOSYNC=true

# Reiniciar MCP
pkill filesystem-ultra
filesystem-ultra
```

---

### **Problema 2: Archivos se copian pero a ruta incorrecta**

**Síntomas**: Archivos aparecen en ubicaciones inesperadas

**Diagnóstico**:
```bash
# Ver conversión de ruta
# (desde Claude Desktop)
wsl_windows_status
```

**Solución**:
```json
{
  "target_mapping": {
    "/home/user/file.txt": "C:\\CustomPath\\file.txt"
  }
}
```

---

### **Problema 3: Performance lenta**

**Síntomas**: Operaciones write/edit lentas

**Causa**: Auto-sync NO debería causar lentitud (es asíncrono)

**Diagnóstico**:
```bash
# Medir tiempo sin auto-sync
unset MCP_WSL_AUTOSYNC
time echo "test" > /home/user/test1.txt

# Medir tiempo CON auto-sync
export MCP_WSL_AUTOSYNC=true
time echo "test" > /home/user/test2.txt

# Diferencia debe ser < 10ms
```

**Solución**:
- Si hay lentitud significativa → Reportar issue
- Verificar que disk no esté lleno
- Verificar permisos de Windows

---

### **Problema 4: Logs molestos**

**Síntomas**: Muchos logs `[AutoSync] Synced: ...`

**Solución**:
```json
{
  "wsl_auto_sync": {
    "enabled": true,
    "silent": true  // ← Activar modo silencioso
  }
}
```

---

## ❓ **FAQ**

### **¿Auto-sync funciona en Windows nativo?**

**No.** Auto-sync solo funciona cuando MCP se ejecuta **dentro de WSL**.

Si ejecutas MCP en Windows nativo, auto-sync se desactiva automáticamente.

---

### **¿Qué pasa si borro un archivo en WSL?**

**Depende de la configuración**:

```json
{
  "sync_on_delete": false  // Default - NO borra en Windows
}
```

```json
{
  "sync_on_delete": true   // Borra también en Windows
}
```

---

### **¿Se pueden sincronizar archivos binarios?**

**Sí.** Auto-sync funciona con cualquier tipo de archivo (texto, binarios, imágenes, etc.).

---

### **¿Cuánto espacio usa auto-sync?**

**Cero.** Auto-sync no duplica archivos innecesariamente. Solo copia lo que cambias.

---

### **¿Puedo sincronizar hacia múltiples ubicaciones?**

**Sí**, con `target_mapping`:

```json
{
  "target_mapping": {
    "/home/user/docs/report.pdf": "C:\\Documents\\report.pdf",
    "/home/user/docs/report.pdf": "D:\\Backup\\report.pdf"
  }
}
```

**Nota**: Actualmente solo soporta 1 destino por archivo.

---

### **¿Funciona con NFS/SAMBA mounts?**

**Sí**, siempre que:
1. Mount esté accesible desde WSL
2. Tengas permisos de escritura

---

### **¿Cómo desactivo auto-sync temporalmente?**

**Opción 1**: Variable de entorno
```bash
unset MCP_WSL_AUTOSYNC
```

**Opción 2**: Herramienta MCP
```json
{
  "tool": "configure_autosync",
  "arguments": {
    "enabled": false
  }
}
```

---

## 📊 **MEJORES PRÁCTICAS**

### ✅ **DO**

1. **Usar `silent: true`** en producción
2. **Configurar `exclude_patterns`** para node_modules, .git, etc.
3. **Usar `only_subdirs`** para limitar sincronización
4. **Verificar estado** con `autosync_status` regularmente

### ❌ **DON'T**

1. **No sincronizar directorios grandes** (ej: `/usr`, `/var`)
2. **No sincronizar archivos sensibles** (ej: `.env`, `secrets.json`)
3. **No depender de sync instantáneo** (hay latencia mínima por ser async)
4. **No usar en producción servers** (solo para desarrollo local)

---

## 🎯 **CONCLUSIÓN**

WSL Auto-Sync hace que trabajar entre WSL y Windows sea **transparente y sin fricción**.

**Activa auto-sync y olvídate de copiar archivos manualmente** ✨

---

**Recursos Adicionales**:
- 📖 [Tests Completos](WSL_AUTO_SYNC_TESTS.md)
- 🐛 [Reportar Issues](https://github.com/scopweb/mcp-filesystem-go-ultra/issues)
- 📧 Soporte: scopweb@example.com

---

**Autor**: Scopweb  
**Versión**: 3.4.0  
**Última actualización**: 2025-11-15
