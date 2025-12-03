#!/bin/bash
# Script para crear y cerrar el issue Bug #10 en GitHub

echo "========================================"
echo "Creando Issue Bug #10 en GitHub"
echo "========================================"
echo ""

# Crear el issue
gh issue create \
  --title "Bug #10: Sistema de Backup y Protección Mejorados - Pérdida de Código en Operaciones Batch" \
  --body-file bug10_issue.md \
  --label "bug,enhancement,high-priority,backup-system"

if [ $? -eq 0 ]; then
    echo ""
    echo "✅ Issue creado exitosamente"
    echo ""
    echo "Esperando 3 segundos antes de cerrar..."
    sleep 3
    echo ""
    echo "========================================"
    echo "Cerrando Issue Bug #10"
    echo "========================================"
    echo ""
    
    # Cerrar el issue con la resolución
    gh issue close 10 --comment-file bug10_resolution.md
    
    if [ $? -eq 0 ]; then
        echo ""
        echo "✅ Issue cerrado exitosamente con la resolución"
        echo ""
        echo "🎉 Bug #10 documentado y cerrado en GitHub"
    else
        echo ""
        echo "❌ Error al cerrar el issue"
        echo "Por favor cierra manualmente: gh issue close 10 --comment-file bug10_resolution.md"
    fi
else
    echo ""
    echo "❌ Error al crear el issue"
    echo "Verifica que tengas GitHub CLI instalado y autenticado"
    echo "Ejecuta: gh auth login"
fi

echo ""
