package mcpserver

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mcp/filesystem-ultra/core"
)

const workspaceRegistryVersion = 1

var (
	workspaceExportCap = 1 << 20
	errGitTruncated    = errors.New("git output truncated")
	workspaceStores    = map[string]*wsStore{}
	workspaceStoreMu   sync.Mutex
)

type wsRecord struct {
	ID           string `json:"workspace_id"`
	SourcePath   string `json:"source_path"`
	DestPath     string `json:"workspace_path"`
	CommonGitDir string `json:"common_git_dir"`
	BaseCommit   string `json:"base_commit"`
	Generation   int    `json:"generation"`
	State        string `json:"lifecycle_state"`
}

type wsFile struct {
	Version int         `json:"version"`
	Items   []*wsRecord `json:"items"`
}

type wsStore struct {
	mu      sync.Mutex
	dir     string
	hooks   string
	items   map[string]*wsRecord
	corrupt bool
}

type wsChange struct {
	Op   string
	Path string
}

func storeFor(engine *core.UltraFastEngine) (*wsStore, error) {
	key := fmt.Sprintf("mem:%p", engine)
	dir := ""
	if engine != nil {
		dir = engine.ReceiptDir()
	}
	if dir != "" {
		dir = filepath.Join(core.CanonicalPath(dir), "workspaces")
		key = "disk:" + core.CanonicalOCCKey(dir)
	}
	workspaceStoreMu.Lock()
	defer workspaceStoreMu.Unlock()
	if s, ok := workspaceStores[key]; ok {
		return s, nil
	}
	s := &wsStore{dir: dir, items: map[string]*wsRecord{}}
	if dir != "" {
		if err := os.MkdirAll(filepath.Join(dir, "empty-hooks"), 0700); err != nil {
			return nil, err
		}
		if err := s.load(); err != nil {
			s.corrupt = true
		}
	} else {
		hooks, err := os.MkdirTemp("", "fsultra-hooks-")
		if err != nil {
			return nil, err
		}
		s.hooks = hooks
	}
	workspaceStores[key] = s
	return s, nil
}

func (s *wsStore) hooksPath() string {
	if s.hooks != "" {
		return s.hooks
	}
	return filepath.Join(s.dir, "empty-hooks")
}

func (s *wsStore) registryMode() string {
	if s.dir == "" {
		return "memory"
	}
	return "disk"
}

func (s *wsStore) load() error {
	b, err := os.ReadFile(filepath.Join(s.dir, "registry.json"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var file wsFile
	if err := json.Unmarshal(b, &file); err != nil {
		return err
	}
	if file.Version != workspaceRegistryVersion {
		return fmt.Errorf("unsupported workspace registry version %d", file.Version)
	}
	for _, item := range file.Items {
		if item == nil || item.ID == "" {
			return fmt.Errorf("corrupt workspace registry")
		}
		s.items[item.ID] = item
	}
	return nil
}

func (s *wsStore) saveLocked() error {
	if s.dir == "" {
		return nil
	}
	items := make([]*wsRecord, 0, len(s.items))
	for _, item := range s.items {
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	b, err := json.MarshalIndent(wsFile{Version: workspaceRegistryVersion, Items: items}, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(s.dir, "registry-*.json")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	_, werr := tmp.Write(b)
	cerr := tmp.Close()
	if werr != nil || cerr != nil {
		_ = os.Remove(tmpName)
		if werr != nil {
			return werr
		}
		return cerr
	}
	return os.Rename(tmpName, filepath.Join(s.dir, "registry.json"))
}

func newWorkspaceID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func wsJSON(v any) (*mcp.CallToolResult, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(string(b)), nil
}

func wsFail(code, message, path string) (*mcp.CallToolResult, error) {
	return pathErrorResult(code, message, path, nil, ""), nil
}

func remotePath(p string) bool {
	p = strings.TrimSpace(p)
	return strings.HasPrefix(p, `\\`) || strings.HasPrefix(p, `//`)
}

func requireLocalAllowed(engine *core.UltraFastEngine, p string) (string, *mcp.CallToolResult) {
	p = core.NormalizePath(strings.TrimSpace(p))
	if p == "" {
		return "", pathErrorResult(errCodeInvalidParams, "path is required", "", nil, "")
	}
	if remotePath(p) {
		return "", pathErrorResult("WORKSPACE_UNSUPPORTED", "UNC and SMB paths are not supported", p, nil, "use a local path")
	}
	if strings.ContainsAny(p, "\r\n\x00") {
		return "", pathErrorResult(errCodeInvalidParams, "path contains a control character", "", nil, "")
	}
	if !engine.IsPathAllowed(p) {
		return "", notAllowedResult(engine, p)
	}
	canon := core.CanonicalPath(p)
	if canon == "" || !engine.IsPathAllowed(canon) || remotePath(canon) {
		return "", notAllowedResult(engine, p)
	}
	return canon, nil
}

func pathInside(parent, child string) bool {
	rel, err := filepath.Rel(parent, child)
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))
}

func gitArgs(hooks string, args ...string) []string {
	prefix := []string{
		"-c", "core.hooksPath=" + hooks,
		"-c", "core.fsmonitor=",
		"-c", "advice.detachedHead=false",
	}
	return append(prefix, args...)
}

func gitText(ctx context.Context, dir, hooks string, args ...string) (string, error) {
	out, err := execGitCommandCtx(ctx, dir, "git", gitArgs(hooks, args...)...)
	if strings.Contains(out, "[TRUNCADO:") {
		return out, errGitTruncated
	}
	return strings.TrimSpace(out), err
}

func gitMissing(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "does not exist") || strings.Contains(msg, "exists on disk, but not in") || strings.Contains(msg, "exit status 1")
}

func samePath(a, b string) bool {
	return core.CanonicalOCCKey(a) == core.CanonicalOCCKey(b)
}

func absGitPath(base, p string) string {
	p = strings.TrimSpace(strings.ReplaceAll(p, "/", string(os.PathSeparator)))
	if p == "" {
		return ""
	}
	if !filepath.IsAbs(p) {
		p = filepath.Join(base, p)
	}
	return core.CanonicalPath(p)
}

func joinInside(root, rel string) (string, error) {
	rel = filepath.Clean(filepath.FromSlash(rel))
	if rel == "." || rel == "" || filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("path escapes workspace")
	}
	return filepath.Join(root, rel), nil
}

func parseNonNeg(args map[string]interface{}, key string, def int) (int, bool, error) {
	v, ok := args[key]
	if !ok {
		return def, false, nil
	}
	switch n := v.(type) {
	case float64:
		if n < 0 || n != float64(int(n)) {
			return 0, true, fmt.Errorf("%s must be a non-negative integer", key)
		}
		return int(n), true, nil
	case int:
		if n < 0 {
			return 0, true, fmt.Errorf("%s must be a non-negative integer", key)
		}
		return n, true, nil
	default:
		return 0, true, fmt.Errorf("%s must be a number", key)
	}
}

func checkGeneration(args map[string]interface{}, got int) *mcp.CallToolResult {
	n, present, err := parseNonNeg(args, "generation", 0)
	if err != nil {
		return pathErrorResult(errCodeInvalidParams, err.Error(), "", nil, "")
	}
	if present && n != got {
		return pathErrorResult(errCodeStaleWorkspace, "generation does not match the workspace registry", "", nil, "call status and retry with the current generation")
	}
	return nil
}

func workspaceBegin(ctx context.Context, engine *core.UltraFastEngine, args map[string]interface{}) (*mcp.CallToolResult, error) {
	source, bad := requireLocalAllowed(engine, stringArg(args, "source_path"))
	if bad != nil {
		return bad, nil
	}
	dest, bad := requireLocalAllowed(engine, stringArg(args, "dest_path"))
	if bad != nil {
		return bad, nil
	}
	if samePath(source, dest) || pathInside(source, dest) || pathInside(dest, source) {
		return wsFail("WORKSPACE_UNSUPPORTED", "dest must be a new directory outside the source repository", dest)
	}
	if engine.IsAllowedPathRoot(dest) {
		return wsFail("WORKSPACE_UNSUPPORTED", "dest must not be an allowed root", dest)
	}
	parent := filepath.Dir(dest)
	info, err := os.Lstat(parent)
	if err != nil || !info.IsDir() {
		return wsFail(errCodeInvalidParams, "dest parent must already exist", parent)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return wsFail("WORKSPACE_UNSUPPORTED", "dest parent is a symlink or junction", parent)
	}
	if _, err := os.Lstat(dest); err == nil {
		return wsFail("WORKSPACE_EXISTS", "dest already exists", dest)
	} else if !errors.Is(err, os.ErrNotExist) {
		return wsFail(errCodeInvalidParams, err.Error(), dest)
	}
	store, err := storeFor(engine)
	if err != nil {
		return wsFail("WORKSPACE_REGISTRY", err.Error(), "")
	}
	if store.corrupt {
		return wsFail("REGISTRY_CORRUPT", "workspace registry cannot be read", store.dir)
	}
	hooks := store.hooksPath()
	top, err := gitText(ctx, source, hooks, "rev-parse", "--show-toplevel")
	if err != nil || !samePath(top, source) {
		return wsFail("WORKSPACE_NOT_GIT", "source_path must be the git toplevel of a local repository", source)
	}
	bare, err := gitText(ctx, source, hooks, "rev-parse", "--is-bare-repository")
	if err != nil || bare == "true" {
		return wsFail("WORKSPACE_UNSUPPORTED", "bare repositories are not supported", source)
	}
	oid, err := gitText(ctx, source, hooks, "rev-parse", "HEAD")
	if err != nil || len(oid) != 40 {
		return wsFail("WORKSPACE_NOT_GIT", "HEAD is not a resolvable commit", source)
	}
	dirty, err := gitText(ctx, source, hooks, "status", "--porcelain=v1", "--untracked-files=no")
	if err != nil {
		if errors.Is(err, errGitTruncated) {
			return wsFail("WORKSPACE_CAPTURE_TRUNCATED", "tracked status did not fit in the git capture cap", source)
		}
		return wsFail("WORKSPACE_NOT_GIT", err.Error(), source)
	}
	if strings.TrimSpace(dirty) != "" {
		return wsFail("WORKSPACE_DIRTY", "tracked changes in the source tree are not copied; commit or stash them outside this tool first", source)
	}
	untrackedOut, err := gitText(ctx, source, hooks, "status", "--porcelain=v1", "--untracked-files=normal")
	untrackedUnknown := errors.Is(err, errGitTruncated)
	if err != nil && !untrackedUnknown {
		return wsFail("WORKSPACE_NOT_GIT", err.Error(), source)
	}
	untracked := 0
	if !untrackedUnknown {
		for _, line := range strings.Split(untrackedOut, "\n") {
			if strings.HasPrefix(line, "??") {
				untracked++
			}
		}
	}
	attr, attrErr := gitText(ctx, source, hooks, "show", oid+":.gitattributes")
	if attrErr == nil && strings.Contains(attr, "filter=") {
		return wsFail("WORKSPACE_UNSUPPORTED", "committed filter attributes are not supported", source)
	}
	if errors.Is(attrErr, errGitTruncated) {
		return wsFail("WORKSPACE_CAPTURE_TRUNCATED", "attribute read was truncated", source)
	}
	filters, ferr := gitText(ctx, source, hooks, "config", "--local", "--get-regexp", "^filter\\.")
	if errors.Is(ferr, errGitTruncated) || strings.TrimSpace(filters) != "" {
		return wsFail("WORKSPACE_UNSUPPORTED", "local filter drivers are not supported", source)
	}
	tree, err := gitText(ctx, source, hooks, "ls-tree", "-r", "--full-tree", oid)
	if errors.Is(err, errGitTruncated) {
		return wsFail("WORKSPACE_CAPTURE_TRUNCATED", "tree listing was truncated; refusing to create a workspace", source)
	}
	if err != nil && strings.TrimSpace(tree) != "" {
		return wsFail("WORKSPACE_NOT_GIT", err.Error(), source)
	}
	if bad := rejectSpecialModes(tree); bad != "" {
		return wsFail("WORKSPACE_UNSUPPORTED", bad, source)
	}
	id, err := newWorkspaceID()
	if err != nil {
		return wsFail("WORKSPACE_REGISTRY", err.Error(), "")
	}
	rec := &wsRecord{ID: id, SourcePath: source, DestPath: dest, BaseCommit: oid, State: "creating", Generation: 0}
	store.mu.Lock()
	store.items[id] = rec
	saveErr := store.saveLocked()
	store.mu.Unlock()
	if saveErr != nil {
		return wsFail("WORKSPACE_REGISTRY", saveErr.Error(), "")
	}
	if _, err := execGitCommandCtx(ctx, source, "git", gitArgs(hooks, "worktree", "add", "--detach", dest, oid)...); err != nil {
		markRecovery(store, id)
		return wsFail("WORKSPACE_GIT", err.Error(), dest)
	}
	head, err := gitText(ctx, dest, hooks, "rev-parse", "HEAD")
	if err != nil || head != oid {
		markRecovery(store, id)
		return wsFail("WORKSPACE_GIT", "worktree HEAD does not match the requested base", dest)
	}
	commonOut, err := gitText(ctx, dest, hooks, "rev-parse", "--git-common-dir")
	if err != nil {
		markRecovery(store, id)
		return wsFail("WORKSPACE_GIT", err.Error(), dest)
	}
	common := absGitPath(dest, commonOut)
	sourceCommon, err := gitText(ctx, source, hooks, "rev-parse", "--git-common-dir")
	if err != nil || !samePath(common, absGitPath(source, sourceCommon)) {
		markRecovery(store, id)
		return wsFail("WORKSPACE_GIT", "worktree does not share the source common git dir", dest)
	}
	store.mu.Lock()
	rec.CommonGitDir = common
	rec.State = "ready"
	rec.Generation = 1
	saveErr = store.saveLocked()
	store.mu.Unlock()
	if saveErr != nil {
		markRecovery(store, id)
		return wsFail("WORKSPACE_REGISTRY", saveErr.Error(), dest)
	}
	return wsJSON(map[string]any{
		"workspace_id":               id,
		"lifecycle_state":            "ready",
		"source_path":                source,
		"workspace_path":             dest,
		"common_git_dir":             common,
		"base_commit":                oid,
		"generation":                 1,
		"registry":                   store.registryMode(),
		"multi_process":              false,
		"detached":                   true,
		"untracked_excluded":         untracked,
		"untracked_excluded_unknown": untrackedUnknown,
		"ignored_excluded":           true,
		"exclusions": []string{
			"tracked dirty rejected before create",
			"untracked files were not copied",
			"ignored files were not copied",
		},
	})
}

func rejectSpecialModes(tree string) string {
	if strings.TrimSpace(tree) == "" {
		return ""
	}
	for _, line := range strings.Split(tree, "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 1 {
			continue
		}
		switch fields[0] {
		case "100644", "100755":
		case "120000":
			return "symlinks are not supported"
		case "160000":
			return "submodules are not supported"
		default:
			return "unsupported git mode " + fields[0]
		}
	}
	return ""
}

func markRecovery(store *wsStore, id string) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if rec := store.items[id]; rec != nil && rec.State != "discarded" {
		rec.State = "recovery_required"
		_ = store.saveLocked()
	}
}

func loadOwned(ctx context.Context, engine *core.UltraFastEngine, args map[string]interface{}) (*wsStore, *wsRecord, *mcp.CallToolResult) {
	id := strings.TrimSpace(stringArg(args, "workspace_id"))
	if id == "" {
		return nil, nil, pathErrorResult(errCodeInvalidParams, "workspace_id is required", "", nil, "")
	}
	store, err := storeFor(engine)
	if err != nil {
		return nil, nil, pathErrorResult("WORKSPACE_REGISTRY", err.Error(), "", nil, "")
	}
	if store.corrupt {
		return nil, nil, pathErrorResult("REGISTRY_CORRUPT", "workspace registry cannot be read", store.dir, nil, "")
	}
	store.mu.Lock()
	rec := store.items[id]
	store.mu.Unlock()
	if rec == nil {
		return nil, nil, pathErrorResult(errCodeNotFound, "unknown workspace_id", "", nil, "begin creates an owned workspace; existing directories are not adopted")
	}
	if bad := checkGeneration(args, rec.Generation); bad != nil {
		return nil, nil, bad
	}
	if !engine.IsPathAllowed(rec.SourcePath) || !engine.IsPathAllowed(rec.DestPath) {
		return nil, nil, notAllowedResult(engine, rec.DestPath)
	}
	return store, rec, nil
}

func observe(ctx context.Context, store *wsStore, rec *wsRecord) (head, hash string, changes []wsChange, identityOK bool, err error) {
	if _, statErr := os.Lstat(rec.DestPath); statErr != nil {
		return "", "", nil, false, statErr
	}
	hooks := store.hooksPath()
	head, err = gitText(ctx, rec.DestPath, hooks, "rev-parse", "HEAD")
	if err != nil {
		return "", "", nil, false, err
	}
	commonOut, err := gitText(ctx, rec.DestPath, hooks, "rev-parse", "--git-common-dir")
	if err != nil {
		return "", "", nil, false, err
	}
	if !samePath(absGitPath(rec.DestPath, commonOut), rec.CommonGitDir) {
		return head, "", nil, false, fmt.Errorf("git dir mismatch")
	}
	changes, hash, err = changeList(ctx, rec.DestPath, hooks, rec.BaseCommit)
	if err != nil {
		return head, "", nil, true, err
	}
	return head, hash, changes, true, nil
}

func changeList(ctx context.Context, dest, hooks, base string) ([]wsChange, string, error) {
	out, err := gitText(ctx, dest, hooks, "diff", "--name-status", "--no-ext-diff", "--no-textconv", "--no-renames", base)
	if err != nil {
		return nil, "", err
	}
	var items []wsChange
	if out != "" {
		for _, line := range strings.Split(out, "\n") {
			line = strings.TrimRight(line, "\r")
			if line == "" {
				continue
			}
			parts := strings.SplitN(line, "\t", 2)
			if len(parts) != 2 || parts[1] == "" {
				return nil, "", fmt.Errorf("unparsed git name-status %q", line)
			}
			op := parts[0]
			switch op {
			case "A":
				op = "add"
			case "M", "T":
				op = "modify"
			case "D":
				op = "delete"
			default:
				return nil, "", fmt.Errorf("unsupported change %s", op)
			}
			items = append(items, wsChange{Op: op, Path: parts[1]})
		}
	}
	others, err := gitText(ctx, dest, hooks, "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return nil, "", err
	}
	seen := map[string]bool{}
	for _, item := range items {
		seen[item.Path] = true
	}
	for _, rel := range strings.Split(others, "\x00") {
		if rel == "" || seen[rel] {
			continue
		}
		items = append(items, wsChange{Op: "add", Path: rel})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Path < items[j].Path })
	var b strings.Builder
	for _, item := range items {
		b.WriteString(item.Op)
		b.WriteByte('\t')
		b.WriteString(item.Path)
		b.WriteByte('\n')
	}
	sum := sha256.Sum256([]byte(b.String()))
	return items, hex.EncodeToString(sum[:]), nil
}

func workspaceStatus(ctx context.Context, engine *core.UltraFastEngine, args map[string]interface{}) (*mcp.CallToolResult, error) {
	store, rec, bad := loadOwned(ctx, engine, args)
	if bad != nil {
		return bad, nil
	}
	body := map[string]any{
		"workspace_id":    rec.ID,
		"lifecycle_state": rec.State,
		"source_path":     rec.SourcePath,
		"workspace_path":  rec.DestPath,
		"common_git_dir":  rec.CommonGitDir,
		"base_commit":     rec.BaseCommit,
		"generation":      rec.Generation,
		"registry":        store.registryMode(),
		"multi_process":   false,
	}
	if rec.State != "ready" && rec.State != "creating" {
		body["identity_ok"] = false
		return wsJSON(body)
	}
	head, hash, changes, ok, err := observe(ctx, store, rec)
	if errors.Is(err, errGitTruncated) {
		body["capture_truncated"] = true
		body["identity_ok"] = false
		return wsJSON(body)
	}
	if !ok || err != nil {
		markRecovery(store, rec.ID)
		body["lifecycle_state"] = "recovery_required"
		body["identity_ok"] = false
		if err != nil {
			body["observe_error"] = err.Error()
		}
		return wsJSON(body)
	}
	tracked := 0
	for _, ch := range changes {
		if ch.Op != "add" {
			tracked++
		}
	}
	body["head"] = head
	body["identity_ok"] = true
	body["status_hash"] = hash
	body["changes"] = len(changes)
	body["tracked_changes"] = tracked
	body["untracked"] = len(changes) - tracked
	body["capture_truncated"] = false
	return wsJSON(body)
}

func workspaceDiff(ctx context.Context, engine *core.UltraFastEngine, args map[string]interface{}) (*mcp.CallToolResult, error) {
	store, rec, bad := loadOwned(ctx, engine, args)
	if bad != nil {
		return bad, nil
	}
	if rec.State != "ready" {
		return wsFail("WORKSPACE_STATE", "diff requires a ready workspace", rec.DestPath)
	}
	offset, _, err := parseNonNeg(args, "offset", 0)
	if err != nil {
		return wsFail(errCodeInvalidParams, err.Error(), "")
	}
	maxFiles, _, err := parseNonNeg(args, "max_files", 20)
	if err != nil {
		return wsFail(errCodeInvalidParams, err.Error(), "")
	}
	if maxFiles < 1 {
		maxFiles = 1
	}
	if maxFiles > 50 {
		maxFiles = 50
	}
	_, hash, changes, ok, err := observe(ctx, store, rec)
	if err != nil || !ok {
		if errors.Is(err, errGitTruncated) {
			return wsFail("WORKSPACE_CAPTURE_TRUNCATED", "change list exceeded the git capture cap", rec.DestPath)
		}
		markRecovery(store, rec.ID)
		return wsFail("WORKSPACE_STATE", "workspace identity could not be verified", rec.DestPath)
	}
	want := strings.TrimSpace(stringArg(args, "expected_status_hash"))
	if offset > 0 && want == "" {
		return pathErrorResult(errCodeHashRequired, "diff continuation requires expected_status_hash", rec.DestPath, nil, "call status or diff with offset 0"), nil
	}
	if want != "" && want != hash {
		return pathErrorResult(errCodeStaleWorkspace, "status_hash changed; the page was not returned", rec.DestPath, nil, "call status and restart the diff"), nil
	}
	if offset > len(changes) {
		offset = len(changes)
	}
	end := offset + maxFiles
	if end > len(changes) {
		end = len(changes)
	}
	hooks := store.hooksPath()
	pages := make([]map[string]any, 0, end-offset)
	for _, ch := range changes[offset:end] {
		item := map[string]any{"op": ch.Op, "path": ch.Path, "truncated": false}
		if ch.Op != "delete" {
			diff, derr := gitText(ctx, rec.DestPath, hooks, "diff", "--no-ext-diff", "--no-textconv", "-U3", rec.BaseCommit, "--", ch.Path)
			if errors.Is(derr, errGitTruncated) || len(diff) > 8192 {
				item["truncated"] = true
				item["review_only"] = true
				if len(diff) > 8192 {
					diff = diff[:8192]
				}
			}
			if ch.Op == "add" && diff == "" {
				item["review_only"] = true
				item["note"] = "untracked file; export returns bytes"
			} else {
				item["diff"] = diff
			}
		}
		pages = append(pages, item)
	}
	next := end
	if next >= len(changes) {
		next = -1
	}
	return wsJSON(map[string]any{
		"workspace_id": rec.ID,
		"base_commit":  rec.BaseCommit,
		"generation":   rec.Generation,
		"status_hash":  hash,
		"offset":       offset,
		"complete":     next < 0,
		"next_offset":  next,
		"files":        pages,
		"note":         "a truncated file diff is review-only and is not an export",
	})
}

func workspaceExport(ctx context.Context, engine *core.UltraFastEngine, args map[string]interface{}) (*mcp.CallToolResult, error) {
	store, rec, bad := loadOwned(ctx, engine, args)
	if bad != nil {
		return bad, nil
	}
	if rec.State != "ready" {
		return wsFail("WORKSPACE_STATE", "export requires a ready workspace", rec.DestPath)
	}
	_, hash, changes, ok, err := observe(ctx, store, rec)
	if errors.Is(err, errGitTruncated) || !ok {
		return wsFail("WORKSPACE_CAPTURE_TRUNCATED", "refusing a partial export", rec.DestPath)
	}
	if err != nil {
		markRecovery(store, rec.ID)
		return wsFail("WORKSPACE_STATE", err.Error(), rec.DestPath)
	}
	files := make([]map[string]any, 0, len(changes))
	used := 0
	for _, ch := range changes {
		item := map[string]any{"path": ch.Path, "op": ch.Op}
		if ch.Op == "delete" {
			item["absent"] = true
			files = append(files, item)
			continue
		}
		full, err := joinInside(rec.DestPath, ch.Path)
		if err != nil {
			return wsFail("EXPORT_UNSUPPORTED", err.Error(), ch.Path)
		}
		info, err := os.Lstat(full)
		if err != nil {
			return wsFail("EXPORT_UNSUPPORTED", err.Error(), full)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return wsFail("EXPORT_UNSUPPORTED", "only regular files can be exported", full)
		}
		if info.Size() > int64(workspaceExportCap) || used+int(info.Size()) > workspaceExportCap {
			return wsFail("EXPORT_TOO_LARGE", fmt.Sprintf("export would exceed %d bytes", workspaceExportCap), full)
		}
		b, err := os.ReadFile(full)
		if err != nil {
			return wsFail("EXPORT_UNSUPPORTED", err.Error(), full)
		}
		sum := sha256.Sum256(b)
		item["size"] = len(b)
		item["sha256"] = hex.EncodeToString(sum[:])
		item["empty"] = len(b) == 0
		if bytesBinary(b) {
			item["binary"] = true
			enc := base64.StdEncoding.EncodeToString(b)
			item["content_base64"] = enc
			used += len(enc)
		} else {
			item["binary"] = false
			item["content"] = string(b)
			used += len(b)
		}
		if used > workspaceExportCap {
			return wsFail("EXPORT_TOO_LARGE", fmt.Sprintf("export would exceed %d bytes", workspaceExportCap), full)
		}
		files = append(files, item)
	}
	canon, err := json.Marshal(map[string]any{
		"workspace_id": rec.ID,
		"base_commit":  rec.BaseCommit,
		"status_hash":  hash,
		"files":        files,
	})
	if err != nil {
		return wsFail("EXPORT_UNSUPPORTED", err.Error(), rec.DestPath)
	}
	if len(canon) > workspaceExportCap {
		return wsFail("EXPORT_TOO_LARGE", fmt.Sprintf("export would exceed %d bytes", workspaceExportCap), rec.DestPath)
	}
	sum := sha256.Sum256(canon)
	return wsJSON(map[string]any{
		"format":       "workspace-export-v1",
		"complete":     true,
		"workspace_id": rec.ID,
		"base_commit":  rec.BaseCommit,
		"generation":   rec.Generation,
		"status_hash":  hash,
		"export_id":    hex.EncodeToString(sum[:]),
		"files":        files,
		"ignored":      "excluded",
		"renames":      "reported as delete plus add",
	})
}

func bytesBinary(b []byte) bool {
	return strings.ContainsRune(string(b), 0) || !utf8.Valid(b)
}

func workspaceDiscard(ctx context.Context, engine *core.UltraFastEngine, args map[string]interface{}) (*mcp.CallToolResult, error) {
	store, rec, bad := loadOwned(ctx, engine, args)
	if bad != nil {
		return bad, nil
	}
	if rec.State == "discarded" {
		return wsJSON(map[string]any{"workspace_id": rec.ID, "lifecycle_state": "discarded", "already_discarded": true})
	}
	if engine.IsAllowedPathRoot(rec.DestPath) || samePath(rec.DestPath, rec.SourcePath) {
		return wsFail("WORKSPACE_NOT_OWNED", "refusing to delete an allowed root or the source tree", rec.DestPath)
	}
	if _, err := os.Lstat(rec.DestPath); errors.Is(err, os.ErrNotExist) {
		store.mu.Lock()
		rec.State = "discarded"
		_ = store.saveLocked()
		store.mu.Unlock()
		return wsJSON(map[string]any{"workspace_id": rec.ID, "lifecycle_state": "discarded", "already_absent": true})
	}
	_, _, changes, ok, err := observe(ctx, store, rec)
	if err != nil || !ok {
		markRecovery(store, rec.ID)
		return wsFail("WORKSPACE_NOT_OWNED", "refusing to delete a path whose git identity does not match the registry", rec.DestPath)
	}
	if len(changes) > 0 {
		return wsFail("WORKSPACE_DIRTY", "discard refuses a workspace with pending changes; there is no force flag", rec.DestPath)
	}
	store.mu.Lock()
	rec.State = "discarding"
	_ = store.saveLocked()
	store.mu.Unlock()
	if _, err := execGitCommandCtx(ctx, rec.SourcePath, "git", gitArgs(store.hooksPath(), "worktree", "remove", "--", rec.DestPath)...); err != nil {
		markRecovery(store, rec.ID)
		return wsFail("WORKSPACE_GIT", "git worktree remove failed; the directory was not deleted by a recursive fallback", rec.DestPath)
	}
	if _, err := os.Lstat(rec.DestPath); err == nil {
		markRecovery(store, rec.ID)
		return wsFail("WORKSPACE_GIT", "dest still exists after git worktree remove", rec.DestPath)
	}
	store.mu.Lock()
	rec.State = "discarded"
	err = store.saveLocked()
	store.mu.Unlock()
	if err != nil {
		return wsFail("WORKSPACE_REGISTRY", err.Error(), rec.DestPath)
	}
	return wsJSON(map[string]any{"workspace_id": rec.ID, "lifecycle_state": "discarded", "source_untouched": true})
}
