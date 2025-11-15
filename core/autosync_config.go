package core

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// AutoSyncConfig holds the configuration for automatic WSL<->Windows syncing
type AutoSyncConfig struct {
	Enabled          bool              `json:"enabled"`
	SyncOnWrite      bool              `json:"sync_on_write"`
	SyncOnEdit       bool              `json:"sync_on_edit"`
	SyncOnDelete     bool              `json:"sync_on_delete"`
	TargetMapping    map[string]string `json:"target_mapping,omitempty"` // Custom path mappings
	ExcludePatterns  []string          `json:"exclude_patterns,omitempty"` // Patterns to exclude from auto-sync
	Silent           bool              `json:"silent"` // If true, don't log sync operations
	OnlySubdirs      []string          `json:"only_subdirs,omitempty"` // Only sync files under these subdirectories
	ConfigVersion    string            `json:"config_version"` // Config file version
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

// AutoSyncManager handles automatic syncing between WSL and Windows
type AutoSyncManager struct {
	config    *AutoSyncConfig
	configMu  sync.RWMutex
	isWSL     bool
	winUser   string
	enabled   bool
	configPath string
}

// NewAutoSyncManager creates a new AutoSyncManager
func NewAutoSyncManager() *AutoSyncManager {
	manager := &AutoSyncManager{
		config: DefaultAutoSyncConfig(),
		enabled: false,
	}

	// Detect environment
	manager.isWSL, manager.winUser = DetectEnvironment()

	// Try to load configuration
	manager.loadConfig()

	return manager
}

// getConfigPath returns the configuration file path
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
	configPath := m.getConfigPath()
	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			// Config file doesn't exist, use defaults
			return nil
		}
		return fmt.Errorf("failed to read config file: %v", err)
	}

	// Parse JSON
	var fileConfig struct {
		WSLAutoSync *AutoSyncConfig `json:"wsl_auto_sync"`
	}

	if err := json.Unmarshal(data, &fileConfig); err != nil {
		return fmt.Errorf("failed to parse config file: %v", err)
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
	configPath := m.getConfigPath()

	// Create directory if needed
	configDir := filepath.Dir(configPath)
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %v", err)
	}

	// Prepare config structure
	fileConfig := struct {
		WSLAutoSync *AutoSyncConfig `json:"wsl_auto_sync"`
	}{
		WSLAutoSync: m.config,
	}

	// Marshal to JSON with indentation
	data, err := json.MarshalIndent(fileConfig, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %v", err)
	}

	// Write to file
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write config file: %v", err)
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
	m.configMu.Lock()
	m.config = config
	m.enabled = config.Enabled && m.isWSL
	m.configMu.Unlock()

	return m.saveConfig()
}

// GetConfig returns a copy of the current configuration
func (m *AutoSyncManager) GetConfig() AutoSyncConfig {
	m.configMu.RLock()
	defer m.configMu.RUnlock()
	return *m.config
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

// AfterWrite is called after a write operation to potentially auto-sync
func (m *AutoSyncManager) AfterWrite(path string) error {
	if !m.IsEnabled() || !m.config.SyncOnWrite {
		return nil
	}

	if !m.ShouldSyncPath(path) {
		return nil
	}

	return m.syncToWindows(path)
}

// AfterEdit is called after an edit operation to potentially auto-sync
func (m *AutoSyncManager) AfterEdit(path string) error {
	if !m.IsEnabled() || !m.config.SyncOnEdit {
		return nil
	}

	if !m.ShouldSyncPath(path) {
		return nil
	}

	return m.syncToWindows(path)
}

// AfterDelete is called after a delete operation to potentially auto-sync
func (m *AutoSyncManager) AfterDelete(path string) error {
	if !m.IsEnabled() || !m.config.SyncOnDelete {
		return nil
	}

	if !m.ShouldSyncPath(path) {
		return nil
	}

	// Convert to Windows path and delete
	winPath, err := WSLToWindows(path)
	if err != nil {
		return nil // Silent fail
	}

	// Delete on Windows side (ignore errors)
	os.Remove(winPath)
	return nil
}

// syncToWindows performs the actual sync operation
func (m *AutoSyncManager) syncToWindows(wslPath string) error {
	// Convert to Windows path
	winPath, err := WSLToWindows(wslPath)
	if err != nil {
		if !m.config.Silent {
			fmt.Fprintf(os.Stderr, "[AutoSync] Failed to convert path %s: %v\n", wslPath, err)
		}
		return nil // Don't fail the operation
	}

	// Check for custom mapping
	m.configMu.RLock()
	if customTarget, exists := m.config.TargetMapping[wslPath]; exists {
		winPath = customTarget
	}
	m.configMu.RUnlock()

	// Perform copy asynchronously to not block the main operation
	go func() {
		if err := CopyFileWithConversion(wslPath, winPath, true); err != nil {
			if !m.config.Silent {
				fmt.Fprintf(os.Stderr, "[AutoSync] Failed to sync %s -> %s: %v\n", wslPath, winPath, err)
			}
		} else {
			if !m.config.Silent {
				fmt.Fprintf(os.Stderr, "[AutoSync] Synced: %s -> %s\n", wslPath, winPath)
			}
		}
	}()

	return nil
}

// GetStatus returns the current auto-sync status
func (m *AutoSyncManager) GetStatus() map[string]interface{} {
	m.configMu.RLock()
	defer m.configMu.RUnlock()

	return map[string]interface{}{
		"enabled":            m.enabled,
		"is_wsl":             m.isWSL,
		"windows_user":       m.winUser,
		"sync_on_write":      m.config.SyncOnWrite,
		"sync_on_edit":       m.config.SyncOnEdit,
		"sync_on_delete":     m.config.SyncOnDelete,
		"config_path":        m.getConfigPath(),
		"exclude_patterns":   m.config.ExcludePatterns,
		"only_subdirs":       m.config.OnlySubdirs,
		"custom_mappings":    m.config.TargetMapping,
		"config_version":     m.config.ConfigVersion,
	}
}
