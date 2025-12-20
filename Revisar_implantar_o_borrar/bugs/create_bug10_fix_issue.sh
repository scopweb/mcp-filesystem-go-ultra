#!/bin/bash
# Script para crear issue de GitHub para Bug #10 Critical Fix

gh issue create \
  --title "🔴 CRITICAL BUG FIX v3.8.1: Risk Assessment Was Not Blocking Operations" \
  --body-file bug10_critical_fix_issue.md \
  --label "bug,critical,security" \
  --assignee scopweb

echo "✅ Issue creado para Bug #10 Critical Fix (v3.8.1)"
