package core

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
)

// AutoSyncConfig holds the configuration for automatic WSL<->Windows syncing
type AutoSyncConfig struct {
	Enabled         bool              `json:"enabled"`
	SyncOnWrite     bool              `json:"sync_on_write"`
	SyncOnEdit      bool              `json:"sync_on_edit"`
	SyncOnDelete    bool              `json:"sync_on_delete"`
	TargetMapping   map[string]string `json:"target_mapping,omitempty"`
	ExcludePatterns []string          `json:"exclude_patterns,omitempty"`
	Silent          bool              `json:"silent"`
	OnlySubdirs     []string          `json:"only_subdirs,omitempty"`
	ConfigVersion   string            `json:"config_version"`
}

// DefaultAutoSyncConfig returns the default auto-sync configuration
func DefaultAutoSyncConfig() *AutoSyncConfig {
	return &AutoSyncConfig{
		Enabled:         false, // Disabled by default for security
		SyncOnWrite:     true,
		SyncOnEdit:      true,
		SyncOnDelete:    false, // Don't delete by default
		TargetMapping:   make(map[string]string),
		ExcludePatterns: []string{},
		Silent:          false,
		OnlySubdirs:     []string{},
		ConfigVersion:   "1.0",
	}
}

func cloneAutoSyncConfig(c *AutoSyncConfig) AutoSyncConfig {
	if c == nil {
		return *DefaultAutoSyncConfig()
	}
	out := *c
	out.TargetMapping = maps.Clone(c.TargetMapping)
	out.ExcludePatterns = slices.Clone(c.ExcludePatterns)
	out.OnlySubdirs = slices.Clone(c.OnlySubdirs)
	return out
}

// AutoSyncManager handles automatic syncing between WSL and Windows
type AutoSyncManager struct {
	config        *AutoSyncConfig
	configMu      sync.RWMutex
	isWSL         bool
	winUser       string
	enabled       bool
	policyBlocked bool
	configPath    string
	allowedPaths  []string // Copied from engine for safety checks during sync
}

// NewAutoSyncManager creates a new AutoSyncManager
func NewAutoSyncManager() *AutoSyncManager {
	manager := &AutoSyncManager{
		config:  DefaultAutoSyncConfig(),
		enabled: false,
	}

	// Detect environment
	manager.isWSL, manager.winUser = DetectEnvironment()

	// Try to load configuration
	manager.loadConfig()

	return manager
}

// SetAllowedPaths allows the engine to provide the list of allowed paths so that
// auto-sync can respect --allowed-paths boundaries when converting and copying files.
func (m *AutoSyncManager) SetAllowedPaths(paths []string) {
	m.configMu.Lock()
	defer m.configMu.Unlock()
	m.allowedPaths = append([]string(nil), paths...)
}

// isTargetAllowed checks if a target path (after conversion) would be allowed
// under the current AllowedPaths policy. If no policy is set, everything is allowed.
func (m *AutoSyncManager) isTargetAllowed(targetPath string) bool {
	m.configMu.RLock()
	allowed := append([]string(nil), m.allowedPaths...)
	m.configMu.RUnlock()
	return targetWithinAllowed(targetPath, allowed)
}

func targetWithinAllowed(targetPath string, allowed []string) bool {
	if len(allowed) == 0 {
		return true
	}
	targetAbs, err := filepath.Abs(NormalizePath(targetPath))
	if err != nil {
		return false
	}
	targetAbs = cleanForContainment(targetAbs)
	for _, base := range allowed {
		baseAbs, err := filepath.Abs(NormalizePath(base))
		if err != nil {
			continue
		}
		baseAbs = cleanForContainment(baseAbs)
		if targetAbs == baseAbs {
			return true
		}
		rel, err := filepath.Rel(baseAbs, targetAbs)
		if err != nil {
			continue
		}
		if rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

func cleanForContainment(p string) string {
	p = filepath.Clean(p)
	if os.PathSeparator == '\\' {
		return strings.ToLower(p)
	}
	return p
}

// getConfigPath returns the configuration file path. Caller must hold configMu.
func (m *AutoSyncManager) getConfigPath() string {
	if m.configPath != "" {
		return m.configPath
	}

	// Try standard locations
	configPaths := []string{
		// 1. XDG_CONFIG_HOME if set
		filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "mcp-filesystem-ultra", "autosync.json"),
		// 2. ~/.config/mcp-filesystem-ultra/autosync.json
		filepath.Join(os.Getenv("HOME"), ".config", "mcp-filesystem-ultra", "autosync.json"),
		// 3. Current directory
		"autosync.json",
	}

	// Return first existing path, or default to ~/.config location
	for _, path := range configPaths {
		if path != "" {
			if _, err := os.Stat(path); err == nil {
				m.configPath = path
				return path
			}
		}
	}

	// Default to ~/.config location
	home := os.Getenv("HOME")
	if home == "" {
		home = "/home/user"
	}
	m.configPath = filepath.Join(home, ".config", "mcp-filesystem-ultra", "autosync.json")
	return m.configPath
}

// loadConfig loads the auto-sync configuration from file
func (m *AutoSyncManager) loadConfig() error {
	// Check environment variable first
	if envEnabled := os.Getenv("MCP_WSL_AUTOSYNC"); envEnabled != "" {
		if envEnabled == "true" || envEnabled == "1" {
			m.config.Enabled = true
			m.enabled = true
			return nil
		}
	}

	// Try to load from config file
	m.configMu.Lock()
	configPath := m.getConfigPath()
	m.configMu.Unlock()
	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			// Config file doesn't exist, use defaults
			return nil
		}
		return fmt.Errorf("failed to read config file: %w", err)
	}

	// Parse JSON
	var fileConfig struct {
		WSLAutoSync *AutoSyncConfig `json:"wsl_auto_sync"`
	}

	if err := json.Unmarshal(data, &fileConfig); err != nil {
		return fmt.Errorf("failed to parse config file: %w", err)
	}

	if fileConfig.WSLAutoSync != nil {
		m.configMu.Lock()
		m.config = fileConfig.WSLAutoSync
		m.enabled = m.config.Enabled && m.isWSL
		m.configMu.Unlock()
	}

	return nil
}

// saveConfig saves the current configuration to file
func (m *AutoSyncManager) saveConfig() error {
	m.configMu.Lock()
	configPath := m.getConfigPath()
	cfg := cloneAutoSyncConfig(m.config)
	m.configMu.Unlock()

	// Create directory if needed
	configDir := filepath.Dir(configPath)
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	// Prepare config structure
	fileConfig := struct {
		WSLAutoSync *AutoSyncConfig `json:"wsl_auto_sync"`
	}{
		WSLAutoSync: &cfg,
	}

	// Marshal to JSON with indentation
	data, err := json.MarshalIndent(fileConfig, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	// Write to file
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}

// SetEnabled enables or disables auto-sync
func (m *AutoSyncManager) SetEnabled(enabled bool) error {
	m.configMu.Lock()
	m.config.Enabled = enabled
	m.enabled = enabled && m.isWSL
	m.configMu.Unlock()

	// Save configuration
	return m.saveConfig()
}

// IsEnabled returns whether auto-sync is enabled
func (m *AutoSyncManager) IsEnabled() bool {
	m.configMu.RLock()
	defer m.configMu.RUnlock()
	return m.enabled
}

// UpdateConfig updates the configuration
func (m *AutoSyncManager) UpdateConfig(config *AutoSyncConfig) error {
	if config == nil {
		return fmt.Errorf("autosync config is nil")
	}
	m.configMu.Lock()
	if len(m.allowedPaths) > 0 {
		for src, dst := range config.TargetMapping {
			if !targetWithinAllowed(dst, m.allowedPaths) {
				m.configMu.Unlock()
				return fmt.Errorf("target mapping %s -> %s points outside allowed paths", src, dst)
			}
		}
	}
	cloned := cloneAutoSyncConfig(config)
	m.config = &cloned
	m.enabled = cloned.Enabled && m.isWSL
	m.configMu.Unlock()

	return m.saveConfig()
}

// GetConfig returns a copy of the current configuration
func (m *AutoSyncManager) GetConfig() AutoSyncConfig {
	m.configMu.RLock()
	defer m.configMu.RUnlock()
	return cloneAutoSyncConfig(m.config)
}

// ShouldSyncPath determines if a given path should be auto-synced
func (m *AutoSyncManager) ShouldSyncPath(path string) bool {
	if !m.IsEnabled() {
		return false
	}

	if !IsWSLPath(path) {
		return false
	}

	m.configMu.RLock()
	defer m.configMu.RUnlock()

	// Check if path matches only_subdirs filter
	if len(m.config.OnlySubdirs) > 0 {
		matched := false
		for _, subdir := range m.config.OnlySubdirs {
			if filepath.HasPrefix(path, subdir) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}

	// Check exclude patterns
	for _, pattern := range m.config.ExcludePatterns {
		if matched, _ := filepath.Match(pattern, filepath.Base(path)); matched {
			return false
		}
	}

	return true
}

// DisableForPolicy stops automatic copies while a file security policy is
// active. Sync can move bytes outside the tool authorization path.
func (m *AutoSyncManager) DisableForPolicy() {
	if m == nil {
		return
	}
	m.configMu.Lock()
	m.policyBlocked = true
	m.enabled = false
	if m.config != nil {
		m.config.Enabled = false
	}
	m.configMu.Unlock()
}

func (m *AutoSyncManager) policyBlocksSync() bool {
	if m == nil {
		return false
	}
	m.configMu.RLock()
	defer m.configMu.RUnlock()
	return m.policyBlocked
}

func (m *AutoSyncManager) syncDecision(want func(AutoSyncConfig) bool) (proceed bool, silent bool) {
	m.configMu.RLock()
	defer m.configMu.RUnlock()
	if m.policyBlocked || !m.enabled || m.config == nil || !want(*m.config) {
		return false, true
	}
	return true, m.config.Silent
}

// AfterWrite is called after a write operation to potentially auto-sync
func (m *AutoSyncManager) AfterWrite(path string) error {
	proceed, _ := m.syncDecision(func(cfg AutoSyncConfig) bool { return cfg.SyncOnWrite })
	if !proceed || !m.ShouldSyncPath(path) {
		return nil
	}
	return m.syncToWindows(path)
}

// AfterEdit is called after an edit operation to potentially auto-sync
func (m *AutoSyncManager) AfterEdit(path string) error {
	proceed, _ := m.syncDecision(func(cfg AutoSyncConfig) bool { return cfg.SyncOnEdit })
	if !proceed || !m.ShouldSyncPath(path) {
		return nil
	}
	return m.syncToWindows(path)
}

// AfterDelete is called after a delete operation to potentially auto-sync
func (m *AutoSyncManager) AfterDelete(path string) error {
	proceed, silent := m.syncDecision(func(cfg AutoSyncConfig) bool { return cfg.SyncOnDelete })
	if !proceed || !m.ShouldSyncPath(path) {
		return nil
	}

	// Convert to Windows path and delete
	winPath, err := WSLToWindows(path)
	if err != nil {
		return nil // Silent fail
	}

	if !m.isTargetAllowed(winPath) {
		if !silent {
			fmt.Fprintf(os.Stderr, "[AutoSync] Blocked delete sync to path outside allowed directories: %s\n", winPath)
		}
		return nil
	}

	// Delete on Windows side (ignore errors)
	os.Remove(winPath)
	return nil
}

// syncToWindows performs the actual sync operation
func (m *AutoSyncManager) syncToWindows(wslPath string) error {
	m.configMu.RLock()
	silent := true
	var custom string
	hasCustom := false
	if m.config != nil {
		silent = m.config.Silent
		custom, hasCustom = m.config.TargetMapping[wslPath]
	}
	m.configMu.RUnlock()

	// Convert to Windows path
	winPath, err := WSLToWindows(wslPath)
	if err != nil {
		if !silent {
			fmt.Fprintf(os.Stderr, "[AutoSync] Failed to convert path %s: %v\n", wslPath, err)
		}
		return nil // Don't fail the operation
	}
	if hasCustom {
		winPath = custom
	}

	if !m.isTargetAllowed(winPath) {
		if !silent {
			fmt.Fprintf(os.Stderr, "[AutoSync] Blocked sync to path outside allowed directories: %s\n", winPath)
		}
		return nil
	}

	// Perform copy asynchronously to not block the main operation
	go func() {
		if err := CopyFileWithConversion(wslPath, winPath, true); err != nil {
			if !silent {
				fmt.Fprintf(os.Stderr, "[AutoSync] Failed to sync %s -> %s: %v\n", wslPath, winPath, err)
			}
			return
		}
		if !silent {
			fmt.Fprintf(os.Stderr, "[AutoSync] Synced: %s -> %s\n", wslPath, winPath)
		}
	}()

	return nil
}

// GetStatus returns the current auto-sync status
func (m *AutoSyncManager) GetStatus() map[string]interface{} {
	m.configMu.Lock()
	cfg := cloneAutoSyncConfig(m.config)
	configPath := m.getConfigPath()
	enabled := m.enabled
	isWSL := m.isWSL
	winUser := m.winUser
	m.configMu.Unlock()

	return map[string]interface{}{
		"enabled":          enabled,
		"is_wsl":           isWSL,
		"windows_user":     winUser,
		"sync_on_write":    cfg.SyncOnWrite,
		"sync_on_edit":     cfg.SyncOnEdit,
		"sync_on_delete":   cfg.SyncOnDelete,
		"config_path":      configPath,
		"exclude_patterns": cfg.ExcludePatterns,
		"only_subdirs":     cfg.OnlySubdirs,
		"custom_mappings":  cfg.TargetMapping,
		"config_version":   cfg.ConfigVersion,
	}
}
