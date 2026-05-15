package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mcp/filesystem-ultra/core"
)

// registerPlatformTools registers wsl, server_info
func registerPlatformTools(reg *toolRegistry) {
	engine := reg.engine

	// ============================================================================
	// 15. wsl — WSL/Windows integration (consolidated: wsl_sync + wsl_status + configure_autosync + autosync_status)
	// ============================================================================
	wslTool := mcp.NewTool("wsl",
		mcp.WithTitleAnnotation("WSL Integration"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithDescription("wsl — WSL/Windows file sync and path conversion. Actions: sync, status, autosync_config, autosync_status. "+
			"Related: read_file, edit_file, copy_file, search_files."),
		mcp.WithString("action", mcp.Description("Action: sync (default), status, autosync_config, autosync_status")),
		// sync params
		mcp.WithString("wsl_path", mcp.Description("Source WSL path for sync")),
		mcp.WithString("windows_path", mcp.Description("Destination or source Windows path for sync")),
		mcp.WithString("direction", mcp.Description("Sync direction: wsl_to_windows, windows_to_wsl, or bidirectional")),
		mcp.WithBoolean("create_dirs", mcp.Description("Create destination directories (default: true)")),
		mcp.WithString("filter_pattern", mcp.Description("Optional file filter pattern for workspace sync")),
		mcp.WithBoolean("dry_run", mcp.Description("Preview changes without executing (default: false)")),
		// autosync_config params
		mcp.WithBoolean("enabled", mcp.Description("Enable/disable auto-sync (for autosync_config)")),
		mcp.WithBoolean("sync_on_write", mcp.Description("Auto-sync on write operations (default: true)")),
		mcp.WithBoolean("sync_on_edit", mcp.Description("Auto-sync on edit operations (default: true)")),
		mcp.WithBoolean("silent", mcp.Description("Silent mode for auto-sync (default: false)")),
	)
	reg.addTool(wslTool, auditWrap(engine, "wsl", func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		action := "sync"
		if args, ok := request.Params.Arguments.(map[string]interface{}); ok {
			if a, ok := args["action"].(string); ok && a != "" {
				action = a
			}
		}

		switch action {
		case "status":
			status, err := engine.GetWSLWindowsStatus(ctx)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("Failed to get status: %v", err)), nil
			}

			if engine.IsCompactMode() {
				env := status["environment"].(string)
				isWSL := status["is_wsl"].(bool)
				return mcp.NewToolResultText(fmt.Sprintf("Env: %s, WSL: %v", env, isWSL)), nil
			}

			var output strings.Builder
			output.WriteString("WSL / Windows Integration Status\n")
			output.WriteString("---\n\n")

			output.WriteString(fmt.Sprintf("Environment: %s\n", status["environment"]))
			output.WriteString(fmt.Sprintf("Running in WSL: %v\n", status["is_wsl"]))

			if winUser, ok := status["windows_user"].(string); ok && winUser != "" {
				output.WriteString(fmt.Sprintf("Windows User: %s\n", winUser))
			}

			output.WriteString("\nPaths:\n")
			output.WriteString(fmt.Sprintf("  WSL Home: %s\n", status["wsl_home"]))
			if wslWinHome, ok := status["windows_home_wsl_style"].(string); ok && wslWinHome != "" {
				output.WriteString(fmt.Sprintf("  Windows Home (WSL style): %s\n", wslWinHome))
			}
			if winHome, ok := status["windows_home_windows_style"].(string); ok && winHome != "" {
				output.WriteString(fmt.Sprintf("  Windows Home (Windows style): %s\n", winHome))
			}

			output.WriteString(fmt.Sprintf("\nSystem:\n"))
			output.WriteString(fmt.Sprintf("  Path Separator: %s\n", status["path_separator"]))

			if interopAvail, ok := status["windows_interop_available"].(bool); ok {
				output.WriteString(fmt.Sprintf("  Windows Interop: %v\n", interopAvail))
			}

			if dirStatus, ok := status["directory_status"].(map[string]bool); ok && len(dirStatus) > 0 {
				output.WriteString("\nDirectory Status:\n")
				for dir, exists := range dirStatus {
					marker := "NO"
					if exists {
						marker = "OK"
					}
					output.WriteString(fmt.Sprintf("  [%s] %s\n", marker, dir))
				}
			}

			return mcp.NewToolResultText(output.String()), nil

		case "autosync_config":
			enabledVal := false
			hasEnabled := false
			if args, ok := request.Params.Arguments.(map[string]interface{}); ok {
				if e, ok := args["enabled"].(bool); ok {
					enabledVal = e
					hasEnabled = true
				}
			}
			if !hasEnabled {
				return mcp.NewToolResultError("enabled parameter is required for autosync_config"), nil
			}

			asCfg := engine.GetAutoSyncConfig()
			asCfg.Enabled = enabledVal

			if args, ok := request.Params.Arguments.(map[string]interface{}); ok {
				if syncOnWrite, ok := args["sync_on_write"].(bool); ok {
					asCfg.SyncOnWrite = syncOnWrite
				}
				if syncOnEdit, ok := args["sync_on_edit"].(bool); ok {
					asCfg.SyncOnEdit = syncOnEdit
				}
				if silent, ok := args["silent"].(bool); ok {
					asCfg.Silent = silent
				}
			}

			if err := engine.SetAutoSyncConfig(asCfg); err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("Failed to configure auto-sync: %v", err)), nil
			}

			isWSL, _ := core.DetectEnvironment()
			if !isWSL && enabledVal {
				return mcp.NewToolResultText("Auto-sync enabled, but not running in WSL. Auto-sync only works in WSL environment."), nil
			}

			if enabledVal {
				if engine.IsCompactMode() {
					return mcp.NewToolResultText("Auto-sync enabled"), nil
				}
				return mcp.NewToolResultText("Auto-sync enabled!\n\nFiles written/edited in WSL will be automatically copied to Windows.\nYou can disable it anytime with: wsl(action:\"autosync_config\", enabled:false)"), nil
			}
			if engine.IsCompactMode() {
				return mcp.NewToolResultText("Auto-sync disabled"), nil
			}
			return mcp.NewToolResultText("Auto-sync disabled. Files will not be automatically synced."), nil

		case "autosync_status":
			asStatus := engine.GetAutoSyncStatus()

			if engine.IsCompactMode() {
				enabled := asStatus["enabled"].(bool)
				isWSL := asStatus["is_wsl"].(bool)
				return mcp.NewToolResultText(fmt.Sprintf("Enabled: %v, WSL: %v", enabled, isWSL)), nil
			}

			var output strings.Builder
			output.WriteString("Auto-Sync Status\n")
			output.WriteString("---\n\n")

			enabled := asStatus["enabled"].(bool)
			isWSL := asStatus["is_wsl"].(bool)

			if enabled {
				output.WriteString("Status: ENABLED\n")
			} else {
				output.WriteString("Status: DISABLED\n")
			}

			output.WriteString(fmt.Sprintf("Environment: %s\n", map[bool]string{true: "WSL", false: "Native"}[isWSL]))

			if winUser, ok := asStatus["windows_user"].(string); ok && winUser != "" {
				output.WriteString(fmt.Sprintf("Windows User: %s\n", winUser))
			}

			output.WriteString("\nConfiguration:\n")
			output.WriteString(fmt.Sprintf("  Sync on Write: %v\n", asStatus["sync_on_write"]))
			output.WriteString(fmt.Sprintf("  Sync on Edit: %v\n", asStatus["sync_on_edit"]))
			output.WriteString(fmt.Sprintf("  Sync on Delete: %v\n", asStatus["sync_on_delete"]))

			if configPath, ok := asStatus["config_path"].(string); ok && configPath != "" {
				output.WriteString(fmt.Sprintf("\nConfig File: %s\n", configPath))
			}

			if !enabled && isWSL {
				output.WriteString("\nTo enable auto-sync, run:\n")
				output.WriteString("   wsl(action:\"autosync_config\", enabled:true)\n")
			}

			if enabled && !isWSL {
				output.WriteString("\nAuto-sync is enabled but you're not in WSL.\n")
				output.WriteString("   Auto-sync only works when running in WSL environment.\n")
			}

			return mcp.NewToolResultText(output.String()), nil

		default:
			// Default: sync behavior
			wslPath := ""
			windowsPath := ""
			direction := ""
			createDirs := true
			filterPattern := ""
			dryRun := false

			if args, ok := request.Params.Arguments.(map[string]interface{}); ok {
				if wp, ok := args["wsl_path"].(string); ok {
					wslPath = wp
				}
				if winp, ok := args["windows_path"].(string); ok {
					windowsPath = winp
				}
				if d, ok := args["direction"].(string); ok {
					direction = d
				}
				if cd, ok := args["create_dirs"].(bool); ok {
					createDirs = cd
				}
				if fp, ok := args["filter_pattern"].(string); ok {
					filterPattern = fp
				}
				if dr, ok := args["dry_run"].(bool); ok {
					dryRun = dr
				}
			}

			// Workspace sync mode (when direction is specified)
			if direction != "" {
				syncResult, err := engine.SyncWorkspace(ctx, direction, filterPattern, dryRun)
				if err != nil {
					return mcp.NewToolResultError(fmt.Sprintf("Sync failed: %v", err)), nil
				}

				if engine.IsCompactMode() {
					syncCount := syncResult["synced_count"].(int)
					errorCount := syncResult["error_count"].(int)
					return mcp.NewToolResultText(fmt.Sprintf("OK: %d files synced, %d errors", syncCount, errorCount)), nil
				}

				var output strings.Builder
				output.WriteString("Workspace Sync Results\n")
				output.WriteString("---\n\n")
				output.WriteString(fmt.Sprintf("Direction: %s\n", syncResult["direction"]))
				if filterPattern != "" {
					output.WriteString(fmt.Sprintf("Filter: %s\n", syncResult["filter_pattern"]))
				}
				if dryRun {
					output.WriteString("Mode: DRY RUN (preview only)\n")
				}
				output.WriteString("\n")

				syncedFiles := syncResult["synced_files"].([]string)
				syncCount := syncResult["synced_count"].(int)
				errorCount := syncResult["error_count"].(int)

				if syncCount > 0 {
					output.WriteString(fmt.Sprintf("Files synced: %d\n", syncCount))
					if syncCount <= 20 {
						for _, file := range syncedFiles {
							output.WriteString(fmt.Sprintf("  - %s\n", file))
						}
					} else {
						for i := 0; i < 10; i++ {
							output.WriteString(fmt.Sprintf("  - %s\n", syncedFiles[i]))
						}
						output.WriteString(fmt.Sprintf("  ... and %d more files\n", syncCount-10))
					}
				} else {
					output.WriteString("No files to sync\n")
				}

				if errorCount > 0 {
					syncErrors := syncResult["errors"].([]string)
					output.WriteString(fmt.Sprintf("\nErrors: %d\n", errorCount))
					if errorCount <= 10 {
						for _, errMsg := range syncErrors {
							output.WriteString(fmt.Sprintf("  - %s\n", errMsg))
						}
					} else {
						for i := 0; i < 5; i++ {
							output.WriteString(fmt.Sprintf("  - %s\n", syncErrors[i]))
						}
						output.WriteString(fmt.Sprintf("  ... and %d more errors\n", errorCount-5))
					}
				}

				return mcp.NewToolResultText(output.String()), nil
			}

			// Single file copy mode
			if wslPath != "" {
				err := engine.WSLWindowsCopy(ctx, wslPath, windowsPath, createDirs)
				if err != nil {
					return mcp.NewToolResultError(fmt.Sprintf("Copy failed: %v", err)), nil
				}
				if windowsPath == "" {
					windowsPath, _ = core.WSLToWindows(wslPath)
				}
				if engine.IsCompactMode() {
					return mcp.NewToolResultText(fmt.Sprintf("OK: Copied to %s", windowsPath)), nil
				}
				return mcp.NewToolResultText(fmt.Sprintf("Successfully copied from WSL to Windows:\n  Source: %s\n  Destination: %s", wslPath, windowsPath)), nil
			}

			if windowsPath != "" {
				wslDest := ""
				if args, ok := request.Params.Arguments.(map[string]interface{}); ok {
					if wp, ok := args["wsl_path"].(string); ok {
						wslDest = wp
					}
				}
				err := engine.WSLWindowsCopy(ctx, windowsPath, wslDest, createDirs)
				if err != nil {
					return mcp.NewToolResultError(fmt.Sprintf("Copy failed: %v", err)), nil
				}
				if wslDest == "" {
					wslDest, _ = core.WindowsToWSL(windowsPath)
				}
				if engine.IsCompactMode() {
					return mcp.NewToolResultText(fmt.Sprintf("OK: Copied to %s", wslDest)), nil
				}
				return mcp.NewToolResultText(fmt.Sprintf("Successfully copied from Windows to WSL:\n  Source: %s\n  Destination: %s", windowsPath, wslDest)), nil
			}

			return mcp.NewToolResultError("For sync: provide wsl_path, windows_path, or direction. Use action:\"status\" for integration status."), nil
		}
	}))

	// ============================================================================
	// 16. server_info — Server info (consolidated: stats + artifact + get_help)
	// ============================================================================
	serverInfoTool := mcp.NewTool("server_info",
		mcp.WithTitleAnnotation("Server Info"),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithDescription("server_info — Server help, performance stats, and artifact capture. Actions: help, stats, artifact. "+
			"Related: edit_file, search_files, batch_operations, backup, analyze_operation."),
		mcp.WithString("action", mcp.Description("Action: help (default), stats, artifact")),
		// help params
		mcp.WithString("topic", mcp.Description("Help topic: overview, workflow, tools, read, write, edit, search, batch, errors, examples, tips, all")),
		// artifact params
		mcp.WithString("sub_action", mcp.Description("For artifact: capture, write, info")),
		mcp.WithString("content", mcp.Description("Artifact content to capture")),
		mcp.WithString("path", mcp.Description("Path for writing artifact")),
	)
	reg.addTool(serverInfoTool, auditWrap(engine, "server_info", func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		action := "help"
		if args, ok := request.Params.Arguments.(map[string]interface{}); ok {
			// server_action is set by fs super-tool remapping; action is used when
			// called directly as server_info tool
			if a, ok := args["server_action"].(string); ok && a != "" {
				action = a
			} else if a, ok := args["action"].(string); ok && a != "" && a != "server_info" {
				action = a
			}
		}

		switch action {
		case "stats":
			stats := engine.GetPerformanceStats()

			// Also include edit telemetry
			summary := engine.GetEditTelemetrySummary()
			data, _ := json.MarshalIndent(summary, "", "  ")
			telemetry := fmt.Sprintf("\n\nEdit Telemetry:\n%s", string(data))

			// Include backup system info
			backupInfo := ""
			if bm := engine.GetBackupManager(); bm != nil {
				backups, _ := bm.ListBackups(1, "all", "", 0)
				maxCount, maxAge := bm.GetBackupLimits()
				backupInfo = fmt.Sprintf("\n\nBackup System:\n  Directory: %s\n  Max count: %d\n  Max age: %d days",
					bm.GetBackupDir(), maxCount, maxAge)
				if len(backups) > 0 {
					backupInfo += fmt.Sprintf("\n  Latest: %s (%s, %s)",
						backups[0].BackupID, backups[0].Operation, core.FormatAge(backups[0].Timestamp))
				}
				allBackups, _ := bm.ListBackups(9999, "all", "", 0)
				backupInfo += fmt.Sprintf("\n  Total backups: %d", len(allBackups))
				backupInfo += fmt.Sprintf("\n  UNDO last edit: backup(action:\"undo_last\")")
			}

			return mcp.NewToolResultText(stats + telemetry + backupInfo), nil

		case "artifact":
			subAction := "info"
			if args, ok := request.Params.Arguments.(map[string]interface{}); ok {
				if sa, ok := args["sub_action"].(string); ok && sa != "" {
					subAction = sa
				}
			}

			switch subAction {
			case "capture":
				content := ""
				if args, ok := request.Params.Arguments.(map[string]interface{}); ok {
					if c, ok := args["content"].(string); ok {
						content = c
					}
				}
				if content == "" {
					return mcp.NewToolResultError("content parameter is required for artifact capture"), nil
				}

				err := engine.CaptureLastArtifact(ctx, content)
				if err != nil {
					return mcp.NewToolResultError(fmt.Sprintf("Error: %v", err)), nil
				}

				lines := strings.Count(content, "\n") + 1
				return mcp.NewToolResultText(fmt.Sprintf("Captured artifact: %d bytes, %d lines", len(content), lines)), nil

			case "write":
				artifactPath := ""
				if args, ok := request.Params.Arguments.(map[string]interface{}); ok {
					if p, ok := args["path"].(string); ok {
						artifactPath = p
					}
				}
				if artifactPath == "" {
					return mcp.NewToolResultError("path parameter is required for artifact write"), nil
				}

				err := engine.WriteLastArtifact(ctx, artifactPath)
				if err != nil {
					return mcp.NewToolResultError(fmt.Sprintf("Error: %v", err)), nil
				}

				return mcp.NewToolResultText(fmt.Sprintf("Wrote last artifact to: %s", artifactPath)), nil

			case "info":
				info := engine.GetLastArtifactInfo()
				return mcp.NewToolResultText(info), nil

			default:
				return mcp.NewToolResultError(fmt.Sprintf("Unknown sub_action: %s. Valid: capture, write, info", subAction)), nil
			}

		case "help":
			topic := "overview"
			if args, ok := request.Params.Arguments.(map[string]interface{}); ok {
				if t, ok := args["topic"].(string); ok && t != "" {
					topic = strings.ToLower(t)
				}
			}

			help := getHelpContent(topic, engine.IsCompactMode())
			return mcp.NewToolResultText(help), nil

		default:
			return mcp.NewToolResultError(fmt.Sprintf("Unknown server_action: %s. Valid: help, stats, artifact (or use sub_action for artifact options)", action)), nil
		}
	}))
}
