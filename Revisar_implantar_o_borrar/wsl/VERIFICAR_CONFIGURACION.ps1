# Script de Verificación de Configuración Claude Desktop + WSL
# Ejecutar en PowerShell como administrador

Write-Host "======================================"
Write-Host "Verificador de Configuración Claude Desktop + WSL"
Write-Host "=====================================" -ForegroundColor Cyan
Write-Host ""

# 1. Verificar si WSL está instalado
Write-Host "1. Verificando instalación de WSL..." -ForegroundColor Yellow
try {
    $wslVersion = wsl --version
    Write-Host "✓ WSL está instalado:" -ForegroundColor Green
    Write-Host $wslVersion
} catch {
    Write-Host "✗ WSL no está instalado o no se puede acceder" -ForegroundColor Red
}
Write-Host ""

# 2. Listar distribuciones de WSL
Write-Host "2. Distribuciones de WSL disponibles:" -ForegroundColor Yellow
try {
    $distributions = wsl --list --verbose
    Write-Host $distributions -ForegroundColor Green
} catch {
    Write-Host "✗ No se pueden listar distribuciones" -ForegroundColor Red
}
Write-Host ""

# 3. Verificar archivo de configuración
Write-Host "3. Buscando archivo de configuración de Claude..." -ForegroundColor Yellow
$claudeConfigPath = "$env:APPDATA\Claude\claude_desktop_config.json"
if (Test-Path $claudeConfigPath) {
    Write-Host "✓ Archivo encontrado en: $claudeConfigPath" -ForegroundColor Green
    Write-Host ""
    Write-Host "Contenido del archivo:" -ForegroundColor Cyan
    Get-Content $claudeConfigPath | ConvertFrom-Json -ErrorAction SilentlyContinue | ConvertTo-Json | Write-Host
} else {
    Write-Host "✗ Archivo no encontrado en: $claudeConfigPath" -ForegroundColor Red
}
Write-Host ""

# 4. Verificar acceso a carpetas comunes
Write-Host "4. Verificando acceso a carpetas de usuario desde WSL..." -ForegroundColor Yellow
$userName = $env:USERNAME
$documentsPath = "$env:USERPROFILE\Documents"
$wslPath = "/mnt/c/Users/$userName/Documents"

Write-Host "Usuario de Windows: $userName" -ForegroundColor Cyan
Write-Host "Ruta en Windows: $documentsPath" -ForegroundColor Cyan
Write-Host "Ruta en WSL: $wslPath" -ForegroundColor Cyan

try {
    $testWSLAccess = wsl test -d $wslPath
    if ($LASTEXITCODE -eq 0) {
        Write-Host "✓ WSL puede acceder a la carpeta Documents" -ForegroundColor Green
    } else {
        Write-Host "✗ WSL no puede acceder a la carpeta Documents" -ForegroundColor Red
    }
} catch {
    Write-Host "✗ Error al verificar acceso desde WSL" -ForegroundColor Red
}
Write-Host ""

# 5. Verificar JSON válido
Write-Host "5. Validando sintaxis JSON del archivo de configuración..." -ForegroundColor Yellow
if (Test-Path $claudeConfigPath) {
    try {
        $jsonContent = Get-Content $claudeConfigPath | ConvertFrom-Json
        Write-Host "✓ JSON válido" -ForegroundColor Green

        # Verificar si tiene la sección de mcpServers
        if ($jsonContent.mcpServers) {
            Write-Host "✓ Sección 'mcpServers' encontrada" -ForegroundColor Green
            Write-Host "  Servidores configurados:" -ForegroundColor Cyan
            $jsonContent.mcpServers | Get-Member -MemberType NoteProperty | ForEach-Object {
                Write-Host "    - $($_.Name)"
            }
        } else {
            Write-Host "✗ No se encontró sección 'mcpServers'" -ForegroundColor Red
        }

        # Verificar variable de entorno MCP_BASE_PATH
        Write-Host ""
        Write-Host "  Variables de entorno configuradas:" -ForegroundColor Cyan
        $jsonContent.mcpServers | Get-Member -MemberType NoteProperty | ForEach-Object {
            $serverName = $_.Name
            $server = $jsonContent.mcpServers.$serverName
            if ($server.env.MCP_BASE_PATH) {
                Write-Host "    ✓ $serverName tiene MCP_BASE_PATH: $($server.env.MCP_BASE_PATH)" -ForegroundColor Green
            } else {
                Write-Host "    ✗ $serverName NO tiene MCP_BASE_PATH configurado" -ForegroundColor Yellow
            }
        }
    } catch {
        Write-Host "✗ Error al parsear JSON: $_" -ForegroundColor Red
    }
} else {
    Write-Host "✗ No se puede validar - archivo no encontrado" -ForegroundColor Red
}
Write-Host ""

# 6. Recomendaciones
Write-Host "======================================"
Write-Host "Recomendaciones" -ForegroundColor Cyan
Write-Host "=====================================" -ForegroundColor Cyan
Write-Host ""
Write-Host "1. Asegúrate de que 'MCP_BASE_PATH' apunta a una carpeta válida"
Write-Host "2. Usa '/mnt/c/' en lugar de 'C:\' en las rutas de WSL"
Write-Host "3. Después de cambiar configuración, reinicia Claude Desktop completamente"
Write-Host "4. Verifica los permisos de carpeta con: chmod 777 /ruta/en/wsl"
Write-Host ""
Write-Host "Para más información, lee:" -ForegroundColor Yellow
Write-Host "  - CONFIGURAR_CLAUDE_DESKTOP_WSL.md (guía completa)"
Write-Host "  - GUIA_RAPIDA_WSL.md (referencia rápida)"
Write-Host ""
Write-Host "======================================"
Write-Host "Verificación completada" -ForegroundColor Green
Write-Host "======================================"
