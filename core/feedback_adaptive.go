package core

import "fmt"

// AdaptiveWriteBackupCreator is the signature of the function that creates a
// pre-write safety backup. It is injected so the adaptive logic can be tested
// without spinning up a real BackupManager. The implementation is expected to
// return the new backup ID and a nil error on success, or ("", err) on failure.
//
// The arguments it receives are: normalized path, operation tag for metadata,
// and human-readable user context. The closure in the caller is responsible
// for capturing chain state (previousBackupID) and passing it to the
// underlying BackupManager — the helper does not need to know about chains.
type AdaptiveWriteBackupCreator func(normPath, op, userContext string) (string, error)

// ApplyAdaptiveWriteBlock implements the adaptive downgrade for the
// `write_file` tool's CheckWriteOp signal. When CheckWriteOp decides to
// block the write (truncation or inflation_loop pattern) and a backup is
// available, this function attempts to create a safety backup via
// createBackup. On success the signal is downgraded to a non-blocking WARN,
// the new backup ID is injected into the Suggestion as a literal restore
// command, and the Downgraded flag is set so tool handlers can pick a
// verbose response format. On failure the original blocking signal is
// returned unchanged (defensive — we never proceed with a destructive
// write without a safety net).
//
// Pure function: no I/O, no engine dependency. The caller in tools_core.go
// wires createBackup to a closure that calls
// engine.GetBackupManager().CreateBackupWithContextAndParent with the
// current chain head captured at call time.
//
// Returns the (possibly downgraded) signal and the new backup ID. The
// backup ID is empty when no downgrade happened (no block, no backup, or
// backup creation failed).
func ApplyAdaptiveWriteBlock(
	signal *FeedbackSignal,
	backupAvailable bool,
	normPath string,
	newSize, existingSize int64,
	createBackup AdaptiveWriteBackupCreator,
) (*FeedbackSignal, string) {
	if signal == nil || !signal.BlockOp || !backupAvailable || createBackup == nil {
		return signal, ""
	}

	userContext := fmt.Sprintf(
		"Adaptive write_file: %d B → %d B (%s)",
		existingSize, newSize, signal.Pattern,
	)

	backupID, backupErr := createBackup(normPath, "write_file_adaptive", userContext)
	if backupErr != nil {
		// Backup failed → keep original block. The caller in tools_core.go
		// is responsible for calling engine.SetCurrentBackupID after a
		// successful downgrade.
		return signal, ""
	}

	// Downgrade: KO → Warn, BlockOp=false, Downgraded=true, and prepend the
	// restore command to the existing suggestion so the user/AI sees the
	// exact undo action.
	signal.Status = FeedbackWarn
	signal.BlockOp = false
	signal.Downgraded = true
	signal.Suggestion = fmt.Sprintf(
		"Backup created: %s. To undo: backup(action:\"restore\", backup_id:\"%s\"). %s",
		backupID, backupID, signal.Suggestion,
	)
	return signal, backupID
}
