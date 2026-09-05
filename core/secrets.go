package core

import (
	"path/filepath"
	"strings"
)

func IsSecretPath(path string) bool {
	base := strings.ToLower(filepath.Base(path))
	if base == ".env" || strings.HasPrefix(base, ".env.") {
		return true
	}
	if base == "credentials.json" || base == ".npmrc" || base == ".pypirc" {
		return true
	}
	if strings.HasPrefix(base, "id_rsa") || strings.HasPrefix(base, "id_ed25519") || strings.HasPrefix(base, "id_ecdsa") {
		return true
	}
	ext := filepath.Ext(base)
	switch ext {
	case ".pem", ".key", ".p12", ".pfx":
		return true
	}
	return false
}
