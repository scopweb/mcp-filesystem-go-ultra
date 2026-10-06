package core

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// SetFilePolicy installs an immutable policy and pins server-owned
// directories (backup, receipts, logs) so tools cannot read stored copies.
// Nil disables the policy. Call once before serving.
func (e *UltraFastEngine) SetFilePolicy(p *FilePolicy) {
	if e == nil {
		return
	}
	if p != nil && p.Enabled() && e.config != nil {
		if e.config.BackupDir != "" {
			p.PinDirectory(e.config.BackupDir, LevelProtected)
		}
		if e.config.ReceiptDir != "" {
			p.PinDirectory(e.config.ReceiptDir, LevelProtected)
		}
		if e.config.LogDir != "" {
			p.PinDirectory(e.config.LogDir, LevelProtected)
		}
	}
	e.filePolicyMu.Lock()
	e.filePolicy = p
	e.filePolicyMu.Unlock()
	if e.backupManager != nil {
		if p != nil && p.Enabled() {
			e.backupManager.SetPolicy(func(path string, op PolicyOp) error {
				return e.Authorize(op, path)
			})
		} else {
			e.backupManager.SetPolicy(nil)
		}
	}
	if p != nil && p.Enabled() && e.autoSyncManager != nil {
		e.autoSyncManager.DisableForPolicy()
	}
}

// FilePolicy returns the process policy, or nil when none was loaded.
func (e *UltraFastEngine) FilePolicy() *FilePolicy {
	if e == nil {
		return nil
	}
	e.filePolicyMu.RLock()
	defer e.filePolicyMu.RUnlock()
	return e.filePolicy
}

// PolicyEnabled reports whether a file security policy is active.
func (e *UltraFastEngine) PolicyEnabled() bool {
	p := e.FilePolicy()
	return p != nil && p.Enabled()
}

// PolicyHash is the opaque rule fingerprint stored with retry receipts.
func (e *UltraFastEngine) PolicyHash() string {
	p := e.FilePolicy()
	if p == nil {
		return ""
	}
	return p.Hash()
}

func (e *UltraFastEngine) policyRoots() []string {
	if e == nil {
		return nil
	}
	empty, bases := e.allowedBases()
	if empty {
		return nil
	}
	return bases
}

// ProtectionLevel is the effective level of path, including ancestors,
// symlink targets, pins, and the .git subsystem block.
func (e *UltraFastEngine) ProtectionLevel(path string) ProtectionLevel {
	if !e.PolicyEnabled() {
		return LevelNormal
	}
	logical, canonical := ResolvePolicyPaths(path)
	level := e.FilePolicy().Level(logical, canonical, e.policyRoots())
	if policyGitBlocked(logical) || policyGitBlocked(canonical) {
		level = maxLevel(level, LevelHidden)
	}
	return level
}

// policyGitBlocked hides .git trees while a policy is active. Git objects
// can contain historical bytes of a protected path and cannot be filtered
// by pathspec. This is a subsystem block, not a configured rule.
func policyGitBlocked(path string) bool {
	for _, seg := range splitSegments(filepath.ToSlash(path)) {
		if foldName(seg) == ".git" {
			return true
		}
	}
	return false
}

// OmitFromDiscovery reports whether a path must be left out of listings,
// trees, searches and counters. It does not increment hidden_count.
func (e *UltraFastEngine) OmitFromDiscovery(path string) bool {
	return e.ProtectionLevel(path) >= LevelHidden
}

// BlockContent reports whether file bytes must not be read or returned.
func (e *UltraFastEngine) BlockContent(path string) bool {
	return e.ProtectionLevel(path) >= LevelProtected
}

// Authorize enforces the policy for one path. A nil error means the
// operation may proceed. Hidden paths return an error that unwraps to
// os.ErrNotExist and does not mention the policy.
func (e *UltraFastEngine) Authorize(op PolicyOp, path string) error {
	if !e.PolicyEnabled() || path == "" {
		return nil
	}
	level := e.ProtectionLevel(path)
	d := Decide(level, op)
	if d.Allow {
		return nil
	}
	return e.policyError(op, path, d)
}

func (e *UltraFastEngine) policyError(op PolicyOp, path string, d PolicyDecision) *FilePolicyError {
	public := path
	if d.Hidden {
		// Keep the caller-supplied path so a direct probe looks like a miss.
		// Never substitute the resolved target.
		return &FilePolicyError{Op: opName(op), Path: public, Level: d.Level, Hidden: true}
	}
	msg := d.Message
	if msg == "" {
		msg = "operation denied by file policy"
	}
	return &FilePolicyError{Op: opName(op), Path: public, Level: d.Level, Message: msg}
}

func opName(op PolicyOp) string {
	switch op {
	case OpDiscover:
		return "discover"
	case OpRead:
		return "read"
	case OpMetadata:
		return "metadata"
	case OpCreate:
		return "create"
	case OpWrite:
		return "write"
	case OpDelete:
		return "delete"
	case OpCopyRead:
		return "copy_read"
	case OpCopyWrite:
		return "copy_write"
	case OpMove:
		return "move"
	case OpRestore:
		return "restore"
	case OpBackupCreate:
		return "backup"
	default:
		return "access"
	}
}

// AuthorizeTree rejects a directory mutation when the path or any descendant
// is not allowed for op. Hidden descendants are not named. The operation
// must not start if this returns an error.
func (e *UltraFastEngine) AuthorizeTree(op PolicyOp, path string) error {
	if err := e.Authorize(op, path); err != nil {
		return err
	}
	if !e.PolicyEnabled() {
		return nil
	}
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() {
		return nil
	}
	var denied *FilePolicyError
	walkErr := filepath.WalkDir(path, func(child string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if child == path {
			return nil
		}
		if err := e.Authorize(op, child); err != nil {
			var pe *FilePolicyError
			if errors.As(err, &pe) && pe.Hidden {
				denied = &FilePolicyError{
					Op:      opName(op),
					Path:    path,
					Level:   LevelProtected,
					Message: "operation denied by file policy",
				}
			} else if pe != nil {
				denied = pe
			} else {
				denied = &FilePolicyError{Op: opName(op), Path: path, Message: "operation denied by file policy"}
			}
			return fs.SkipAll
		}
		return nil
	})
	if denied != nil {
		return denied
	}
	return walkErr
}

// RedactedInfo is the minimum listing record for a protected path.
func RedactedInfo(name string, isDir bool, level ProtectionLevel) string {
	kind := "file"
	if isDir {
		kind = "dir"
	}
	if level == LevelNormal {
		return fmt.Sprintf("%s %s", kind, name)
	}
	return fmt.Sprintf("%s %s [%s]", kind, name, level.String())
}

// PolicyCreateOrWrite picks create vs write based on existence, then authorizes.
func (e *UltraFastEngine) PolicyCreateOrWrite(path string) error {
	if !e.PolicyEnabled() {
		return nil
	}
	op := OpWrite
	if _, err := os.Lstat(path); err != nil {
		if os.IsNotExist(err) {
			op = OpCreate
		}
	}
	return e.Authorize(op, path)
}

// FilterBackupOriginal reports whether a backup of originalPath may be listed
// or have its bytes returned. Hidden paths are omitted. Protected paths may
// be named but their bytes must not be returned.
func (e *UltraFastEngine) FilterBackupOriginal(path string, wantBytes bool) error {
	if !e.PolicyEnabled() || path == "" {
		return nil
	}
	if wantBytes {
		if e.BlockContent(path) {
			return e.Authorize(OpRead, path)
		}
		return e.Authorize(OpRestore, path)
	}
	if e.OmitFromDiscovery(path) {
		return e.Authorize(OpDiscover, path)
	}
	return nil
}

// GitPolicyBlock is the generic rejection used when git cannot honor the policy.
func GitPolicyBlock() error {
	return &FilePolicyError{
		Op:      "git",
		Message: "git is unavailable while a file security policy is active",
	}
}

// WSLPolicyBlock is the generic rejection for sync while a policy is active.
func WSLPolicyBlock() error {
	return &FilePolicyError{
		Op:      "wsl",
		Message: "wsl sync is unavailable while a file security policy is active",
	}
}

// GitHubIssuesResolveBlock refuses working-tree repo discovery while a file
// security policy is active. It does not weaken GitPolicyBlock. An explicit
// owner/repo does not run git and is not a bypass of that block.
func GitHubIssuesResolveBlock() error {
	return &FilePolicyError{
		Op:      "github_issues",
		Message: "cannot resolve a GitHub repository from the working tree while a file security policy is active; git is unavailable. Pass repo (owner/repo) and hostname if the host is not github.com",
	}
}

// ContainsPolicyPath reports whether text names a hidden path. Used to keep
// aggregated errors from echoing a hidden descendant.
func (e *UltraFastEngine) SanitizePolicyText(text, requested string) string {
	if !e.PolicyEnabled() || text == "" {
		return text
	}
	if e.OmitFromDiscovery(requested) {
		return "file does not exist"
	}
	return text
}

func (e *UltraFastEngine) policySkipWalk(path string, isDir bool) bool {
	if !e.PolicyEnabled() {
		return false
	}
	if e.OmitFromDiscovery(path) {
		return true
	}
	return false
}

// visibleDirEntries drops hidden names and marks protected entries.
type visibleEntry struct {
	name       string
	isDir      bool
	level      ProtectionLevel
	entry      os.DirEntry
}

func (e *UltraFastEngine) visibleDirEntries(dir string, entries []os.DirEntry) []visibleEntry {
	out := make([]visibleEntry, 0, len(entries))
	for _, entry := range entries {
		child := filepath.Join(dir, entry.Name())
		level := LevelNormal
		if e.PolicyEnabled() {
			level = e.ProtectionLevel(child)
			if level >= LevelHidden {
				continue
			}
		}
		out = append(out, visibleEntry{
			name:  entry.Name(),
			isDir: entry.IsDir(),
			level: level,
			entry: entry,
		})
	}
	return out
}

func (e *UltraFastEngine) annotateName(name string, level ProtectionLevel) string {
	if level == LevelNormal || level == LevelHidden {
		return name
	}
	return name + " [" + level.String() + "]"
}

// receiptPolicyMismatch refuses a stored retry body when it was recorded
// under a different policy fingerprint.
func (e *UltraFastEngine) receiptPolicyMismatch(stored string) bool {
	if !e.PolicyEnabled() {
		return false
	}
	return stored != e.PolicyHash()
}
