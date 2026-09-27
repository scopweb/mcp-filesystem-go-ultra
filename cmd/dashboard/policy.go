package main

import (
	"fmt"
	"strings"

	"github.com/mcp/filesystem-ultra/core"
)

// dashPolicy is nil unless --file-security-config was loaded. The MCP server
// flag does not apply to this process.
var (
	dashPolicy *core.FilePolicy
	dashRoots  []string
)

func loadDashboardPolicy(configPath, allowed string) error {
	configPath = strings.TrimSpace(configPath)
	if configPath == "" {
		return nil
	}
	p, err := core.LoadFilePolicy(configPath)
	if err != nil {
		return err
	}
	for _, root := range strings.Split(allowed, ",") {
		root = strings.TrimSpace(root)
		if root != "" {
			dashRoots = append(dashRoots, root)
		}
	}
	if p.HasRelativeRules() && len(dashRoots) == 0 {
		return fmt.Errorf("dashboard --file-security-config has separator patterns; pass the same --allowed-paths roots used by the MCP server")
	}
	if err := p.PinConfig(configPath); err != nil {
		return err
	}
	dashPolicy = p
	return nil
}

func dashLevel(path string) core.ProtectionLevel {
	if dashPolicy == nil || path == "" {
		return core.LevelNormal
	}
	logical, canonical := core.ResolvePolicyPaths(path)
	return dashPolicy.Level(logical, canonical, dashRoots)
}

func dashHides(path string) bool {
	return dashLevel(path) >= core.LevelHidden
}

func dashBlocksBytes(path string) bool {
	return dashLevel(path) >= core.LevelProtected
}

func dashRestoreDenied(path string) (hidden, denied bool) {
	level := dashLevel(path)
	if level >= core.LevelHidden {
		return true, true
	}
	if level >= core.LevelReadOnly {
		return false, true
	}
	return false, false
}

// dashboardBackupBlocked reports whether a backup file must not be served.
// Unknown originals are blocked while a policy is active (fail closed).
func dashboardBackupBlocked(backupDir, id, fileName string) (original string, blocked bool) {
	if dashPolicy == nil {
		return "", false
	}
	for _, b := range loadAllBackups(backupDir) {
		if b.BackupID != id {
			continue
		}
		for _, f := range b.Files {
			if filepathBase(f.BackupPath) == fileName || filepathBase(f.OriginalPath) == fileName {
				if dashHides(f.OriginalPath) || dashBlocksBytes(f.OriginalPath) {
					return f.OriginalPath, true
				}
				return f.OriginalPath, false
			}
		}
	}
	return "", true
}

func redactDashboardBackups(in []BackupInfo) []BackupInfo {
	if dashPolicy == nil {
		return in
	}
	out := make([]BackupInfo, 0, len(in))
	for _, b := range in {
		files := make([]BackupMetadata, 0, len(b.Files))
		for _, f := range b.Files {
			if dashHides(f.OriginalPath) {
				continue
			}
			if dashBlocksBytes(f.OriginalPath) {
				f.Hash = ""
				f.Size = 0
			}
			files = append(files, f)
		}
		if len(files) == 0 {
			continue
		}
		b.Files = files
		out = append(out, b)
	}
	return out
}

func filepathBase(p string) string {
	p = strings.ReplaceAll(p, "\\", "/")
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[i+1:]
	}
	return p
}
