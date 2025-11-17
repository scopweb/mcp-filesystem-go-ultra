# 📝 Ejemplos de Configuración - WSL Auto-Sync

> Configuraciones listas para copiar y pegar

---

## 🎯 **CONFIGURACIÓN BÁSICA**

### **Ejemplo 1: Activación Simple**

```json
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
```

**Ubicación**: `~/.config/mcp-filesystem-ultra/autosync.json`

**Uso**: Sincronización básica sin filtros

---

## 🚀 **DESARROLLO WEB**

### **Ejemplo 2: Proyecto Node.js**

```json
{
  "wsl_auto_sync": {
    "enabled": true,
    "sync_on_write": true,
    "sync_on_edit": true,
    "sync_on_delete": false,
    "silent": true,
    "exclude_patterns": [
      "node_modules/*",
      "*.log",
      ".git/*",
      ".cache/*",
      "dist/*",
      "build/*",
      "*.tmp"
    ],
    "only_subdirs": [
      "/home/user/projects"
    ],
    "config_version": "1.0"
  }
}
```

**Características**:
- ✅ Excluye `node_modules`
- ✅ Solo sincroniza `/projects`
- ✅ Modo silencioso
- ✅ Ignora archivos temporales

---

### **Ejemplo 3: Proyecto React/Next.js**

```json
{
  "wsl_auto_sync": {
    "enabled": true,
    "sync_on_write": true,
    "sync_on_edit": true,
    "silent": true,
    "exclude_patterns": [
      "node_modules/*",
      ".next/*",
      ".cache/*",
      "out/*",
      "dist/*",
      "build/*",
      "*.log",
      ".git/*",
      ".turbo/*",
      ".vercel/*"
    ],
    "only_subdirs": [
      "/home/user/projects/webapp"
    ],
    "config_version": "1.0"
  }
}
```

---

## 🐍 **DESARROLLO PYTHON**

### **Ejemplo 4: Proyecto Python/Django**

```json
{
  "wsl_auto_sync": {
    "enabled": true,
    "sync_on_write": true,
    "sync_on_edit": true,
    "silent": true,
    "exclude_patterns": [
      "__pycache__/*",
      "*.pyc",
      "*.pyo",
      ".pytest_cache/*",
      ".venv/*",
      "venv/*",
      "env/*",
      "*.egg-info/*",
      ".mypy_cache/*",
      ".tox/*",
      "*.log",
      ".git/*",
      "db.sqlite3",
      "media/*"
    ],
    "only_subdirs": [
      "/home/user/projects/django-app"
    ],
    "config_version": "1.0"
  }
}
```

**Características**:
- ✅ Excluye cache de Python
- ✅ Excluye entornos virtuales
- ✅ Excluye DB SQLite
- ✅ Excluye media files

---

## 🦀 **DESARROLLO GO**

### **Ejemplo 5: Proyecto Go**

```json
{
  "wsl_auto_sync": {
    "enabled": true,
    "sync_on_write": true,
    "sync_on_edit": true,
    "silent": true,
    "exclude_patterns": [
      "vendor/*",
      "*.exe",
      "*.test",
      ".git/*",
      "*.log"
    ],
    "only_subdirs": [
      "/home/user/go/src",
      "/home/user/projects/go-app"
    ],
    "config_version": "1.0"
  }
}
```

---

## 💼 **CONFIGURACIONES PROFESIONALES**

### **Ejemplo 6: Multi-Proyecto con Mappings Custom**

```json
{
  "wsl_auto_sync": {
    "enabled": true,
    "sync_on_write": true,
    "sync_on_edit": true,
    "sync_on_delete": false,
    "silent": true,
    "exclude_patterns": [
      "node_modules/*",
      "__pycache__/*",
      ".git/*",
      "*.log",
      "*.tmp",
      ".cache/*"
    ],
    "only_subdirs": [
      "/home/user/projects",
      "/home/user/documents"
    ],
    "target_mapping": {
      "/home/user/projects/config/prod.json": "D:\\Production\\config.json",
      "/home/user/documents/reports": "C:\\Reports"
    },
    "config_version": "1.0"
  }
}
```

**Características**:
- ✅ Múltiples proyectos
- ✅ Mappings custom para archivos críticos
- ✅ Separación de entornos

---

### **Ejemplo 7: Desarrollo + Backups**

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
      ".git/*"
    ],
    "only_subdirs": [
      "/home/user/projects",
      "/home/user/backups"
    ],
    "target_mapping": {
      "/home/user/backups": "D:\\Backups"
    },
    "config_version": "1.0"
  }
}
```

---

## 📊 **CONFIGURACIONES POR CASO DE USO**

### **Ejemplo 8: Solo Documentos**

```json
{
  "wsl_auto_sync": {
    "enabled": true,
    "sync_on_write": true,
    "sync_on_edit": true,
    "silent": true,
    "exclude_patterns": [
      "*.tmp",
      "~*"
    ],
    "only_subdirs": [
      "/home/user/documents",
      "/home/user/Desktop"
    ],
    "config_version": "1.0"
  }
}
```

**Uso**: Sincronizar solo documentos personales

---

### **Ejemplo 9: Solo Logs**

```json
{
  "wsl_auto_sync": {
    "enabled": true,
    "sync_on_write": true,
    "sync_on_edit": false,
    "silent": true,
    "only_subdirs": [
      "/home/user/logs",
      "/var/log/myapp"
    ],
    "target_mapping": {
      "/var/log/myapp": "C:\\Logs\\MyApp"
    },
    "config_version": "1.0"
  }
}
```

**Uso**: Monitoreo de logs en tiempo real

---

### **Ejemplo 10: CI/CD Artifacts**

```json
{
  "wsl_auto_sync": {
    "enabled": true,
    "sync_on_write": true,
    "sync_on_edit": false,
    "silent": true,
    "only_subdirs": [
      "/home/user/builds",
      "/home/user/dist"
    ],
    "target_mapping": {
      "/home/user/builds": "C:\\CI\\Builds",
      "/home/user/dist": "C:\\CI\\Dist"
    },
    "config_version": "1.0"
  }
}
```

**Uso**: Artifacts de build disponibles en Windows

---

## 🎨 **CONFIGURACIONES EXTREMAS**

### **Ejemplo 11: Máxima Exclusión (Performance)**

```json
{
  "wsl_auto_sync": {
    "enabled": true,
    "sync_on_write": true,
    "sync_on_edit": true,
    "silent": true,
    "exclude_patterns": [
      "node_modules/*",
      "__pycache__/*",
      ".venv/*",
      "venv/*",
      "env/*",
      ".git/*",
      ".svn/*",
      ".hg/*",
      "*.pyc",
      "*.pyo",
      "*.log",
      "*.tmp",
      "*.swp",
      "*.swo",
      "*~",
      ".DS_Store",
      "Thumbs.db",
      ".cache/*",
      ".next/*",
      ".nuxt/*",
      "dist/*",
      "build/*",
      "target/*",
      "vendor/*",
      "*.exe",
      "*.dll",
      "*.so",
      "*.dylib"
    ],
    "only_subdirs": [
      "/home/user/projects/active-project"
    ],
    "config_version": "1.0"
  }
}
```

**Uso**: Proyectos muy grandes, solo sincronizar código fuente

---

### **Ejemplo 12: Sync Selectivo por Extensión**

```json
{
  "wsl_auto_sync": {
    "enabled": true,
    "sync_on_write": true,
    "sync_on_edit": true,
    "silent": true,
    "exclude_patterns": [
      "*",
      "!*.go",
      "!*.mod",
      "!*.sum",
      "!*.md",
      "!*.json"
    ],
    "only_subdirs": [
      "/home/user/projects/go-app"
    ],
    "config_version": "1.0"
  }
}
```

**Nota**: Sintaxis `!` para inclusión requiere soporte del sistema. Verificar antes de usar.

---

## 🔧 **CONFIGURACIONES DE DEBUG**

### **Ejemplo 13: Debug Mode (Ver Todo)**

```json
{
  "wsl_auto_sync": {
    "enabled": true,
    "sync_on_write": true,
    "sync_on_edit": true,
    "sync_on_delete": true,
    "silent": false,
    "exclude_patterns": [],
    "only_subdirs": [],
    "config_version": "1.0"
  }
}
```

**Uso**: Ver todos los logs y sincronizar TODO (para debugging)

---

## 📝 **PLANTILLAS RÁPIDAS**

### **Copiar y Pegar Rápido**

**Bash Script para generar config**:

```bash
#!/bin/bash
# setup-autosync.sh

CONFIG_DIR=~/.config/mcp-filesystem-ultra
CONFIG_FILE=$CONFIG_DIR/autosync.json

mkdir -p $CONFIG_DIR

cat > $CONFIG_FILE << 'EOF'
{
  "wsl_auto_sync": {
    "enabled": true,
    "sync_on_write": true,
    "sync_on_edit": true,
    "sync_on_delete": false,
    "silent": true,
    "exclude_patterns": [
      "node_modules/*",
      "__pycache__/*",
      ".git/*",
      "*.log",
      "*.tmp"
    ],
    "only_subdirs": [
      "/home/$USER/projects"
    ],
    "config_version": "1.0"
  }
}
EOF

echo "✅ Config creado en: $CONFIG_FILE"
cat $CONFIG_FILE
```

**Uso**:
```bash
chmod +x setup-autosync.sh
./setup-autosync.sh
```

---

## 🎯 **RECOMENDACIONES POR LENGUAJE**

| Lenguaje | Exclude Patterns Recomendados |
|----------|-------------------------------|
| **JavaScript/Node** | `node_modules/*, dist/*, build/*, .next/*, .nuxt/*` |
| **Python** | `__pycache__/*, .venv/*, *.pyc, .pytest_cache/*` |
| **Go** | `vendor/*, *.exe, *.test` |
| **Rust** | `target/*, Cargo.lock` |
| **Java** | `target/*, .gradle/*, build/*, *.class` |
| **C/C++** | `*.o, *.obj, *.exe, *.dll, build/*, cmake-build-*` |
| **.NET** | `bin/*, obj/*, *.exe, *.dll` |

---

## ✅ **VALIDACIÓN DE CONFIGURACIÓN**

**Script para validar JSON**:

```bash
#!/bin/bash
# validate-config.sh

CONFIG_FILE=~/.config/mcp-filesystem-ultra/autosync.json

if [ ! -f "$CONFIG_FILE" ]; then
    echo "❌ Config no encontrado: $CONFIG_FILE"
    exit 1
fi

# Validar JSON syntax
if jq empty "$CONFIG_FILE" 2>/dev/null; then
    echo "✅ JSON válido"
    echo ""
    echo "📋 Configuración actual:"
    jq '.wsl_auto_sync' "$CONFIG_FILE"
else
    echo "❌ JSON inválido"
    exit 1
fi
```

---

**Autor**: Scopweb  
**Versión**: 3.4.0  
**Fecha**: 2025-11-15
