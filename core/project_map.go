package core

// project_map.go — cheap structural orientation for a project tree.
//
// project_map answers ONE question: "what shape does this project have?".
// It walks metadata only (names, types, sizes), never file contents, and it
// is bounded on three axes (entries, wall time, accumulated metadata bytes)
// so a huge repository cannot turn an orientation call into a long scan.
//
// It is NOT a code-selection tool: picking the files that matter for a task
// is context_pack / search_files territory. project_map only removes the
// list_directory + directory_tree round-trips an agent would otherwise spend
// on its first call.
//
// Safety rules baked into the walk:
//   - only paths inside the allowed roots are visited (os.Root containment);
//   - symlinks are never followed, so a link cannot escape the root;
//   - paths the file-security policy hides are skipped entirely: their names,
//     sizes and counts never leave the server;
//   - protected entries expose name and type only (no size, no contents).

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Project map defaults and hard limits.
const (
	// ProjectMapDefaultDepth is the default recursion depth.
	ProjectMapDefaultDepth = 2
	// ProjectMapMaxDepth is the deepest recursion a caller may ask for.
	ProjectMapMaxDepth = 3
	// ProjectMapMaxEntries caps how many entries one walk may visit.
	ProjectMapMaxEntries = 5000
	// ProjectMapMaxTime caps the wall time of one walk.
	ProjectMapMaxTime = 2500 * time.Millisecond
	// ProjectMapMaxMetadataBytes caps the accumulated metadata carried in the
	// answer (path text plus a fixed per-entry overhead).
	ProjectMapMaxMetadataBytes = 512 * 1024
	// ProjectMapMaxLargestFiles caps the largest_files list.
	ProjectMapMaxLargestFiles = 20
	// ProjectMapMaxKeyFiles caps the key_files list.
	ProjectMapMaxKeyFiles = 40
	// ProjectMapMaxNotable caps the notable-children list of one directory.
	ProjectMapMaxNotable = 6
)

// ProjectMapSummaryScope is the scope of every counter in the summary. It is
// always "visited_entries": totals describe what the walk actually reached,
// never what the repository contains in full.
const ProjectMapSummaryScope = "visited_entries"

// ProjectMapOpts bounds one walk. Zero values select the defaults.
type ProjectMapOpts struct {
	Depth            int
	Detail           string // summary | normal | full
	MaxEntries       int
	MaxTime          time.Duration
	MaxBytesMetadata int64
}

// ProjectMapEntry is one node of the structure skeleton.
type ProjectMapEntry struct {
	Path     string   `json:"path"`
	Type     string   `json:"type"` // dir | file
	Children int      `json:"children,omitempty"`
	Notable  []string `json:"notable,omitempty"`
}

// ProjectMapFile is a size-bearing file reference.
type ProjectMapFile struct {
	Path string `json:"path"`
	Size int64  `json:"size"`
}

// ProjectMapSummary aggregates what the walk visited.
type ProjectMapSummary struct {
	VisitedFiles int              `json:"visited_files"`
	VisitedDirs  int              `json:"visited_dirs"`
	ByExtension  map[string]int   `json:"by_extension"`
	LargestFiles []ProjectMapFile `json:"largest_files"`
	KeyFiles     []string         `json:"key_files"`
}

// ProjectMapLimits echoes the limits that were in force for the walk.
type ProjectMapLimits struct {
	MaxEntries       int   `json:"max_entries"`
	MaxDepth         int   `json:"max_depth"`
	MaxTimeMS        int64 `json:"max_time_ms"`
	MaxBytesMetadata int64 `json:"max_bytes_metadata"`
	ElapsedMS        int64 `json:"elapsed_ms"`
	VisitedEntries   int   `json:"visited_entries"`
}

// ProjectMapPolicy is the small, already-public policy summary: the same
// facts list_allowed_directories and security_policy expose. It never names
// policy patterns or protected paths.
type ProjectMapPolicy struct {
	Profile   string   `json:"profile"`
	RootsMode string   `json:"roots_mode"`
	AutoOCC   string   `json:"auto_occ"`
	ReadOnly  bool     `json:"readonly"`
	Notes     []string `json:"notes"`
}

// ProjectMapResult is the typed answer of one walk.
type ProjectMapResult struct {
	Roots        []string
	Truncated    bool
	Limits       ProjectMapLimits
	SummaryScope string
	Summary      ProjectMapSummary
	Structure    []ProjectMapEntry
	Policy       ProjectMapPolicy
	Warnings     []string
}

// pmNode is one node of the walk tree. The tree is flattened pre-order at
// the end so a parent always precedes its children.
type pmNode struct {
	entry    ProjectMapEntry
	children []*pmNode
}

// projectMapWalk is the internal accumulator for one walk.
type projectMapWalk struct {
	engine    *UltraFastEngine
	opts      ProjectMapOpts
	root      string
	ignore    *IgnoreMatcher
	start     time.Time
	deadline  time.Time
	entries   int
	metaBytes int64
	truncated bool
	warnings  []string

	dirs          int
	files         int
	skippedIgnore int
	byExt         map[string]int
	largest       []ProjectMapFile
	key           []string
	tree          *pmNode
}

// keyFileNames are the file names that identify a project at a glance.
var keyFileNames = map[string]bool{
	"readme.md": true, "readme": true, "readme.txt": true,
	"license": true, "license.md": true, "license.txt": true,
	"makefile": true, "dockerfile": true, "docker-compose.yml": true,
	"docker-compose.yaml": true, "compose.yml": true, "compose.yaml": true,
	"go.mod": true, "go.sum": true, "package.json": true, "tsconfig.json": true,
	"cargo.toml": true, "pyproject.toml": true, "requirements.txt": true,
	"pom.xml": true, "build.gradle": true, "build.gradle.kts": true,
	"gemfile": true, "justfile": true, "cmakelists.txt": true,
	".gitignore": true, ".gitlab-ci.yml": true,
}

// keyFileSuffixes mark a file as project-defining by extension.
var keyFileSuffixes = []string{
	".sln", ".csproj", ".fsproj", ".vbproj", ".gradle", ".workspace",
}

// keyFileMarkers are path shapes that identify a project by convention.
var keyFileMarkers = []string{
	".github/workflows/", ".circleci/", "azure-pipelines",
}

// sourceExtensions are the extensions that make a directory look like code.
var sourceExtensions = map[string]bool{
	".go": true, ".cs": true, ".ts": true, ".tsx": true, ".js": true,
	".jsx": true, ".py": true, ".rs": true, ".java": true, ".kt": true,
	".c": true, ".h": true, ".cc": true, ".cpp": true, ".hpp": true,
	".rb": true, ".php": true, ".swift": true, ".m": true, ".scala": true,
}

// codeContainerNames are directory names that hold code by convention.
var codeContainerNames = map[string]bool{
	"cmd": true, "core": true, "internal": true, "pkg": true, "src": true,
	"lib": true, "app": true, "api": true, "server": true, "client": true,
	"tests": true, "test": true, "services": true, "models": true,
	"controllers": true, "components": true, "scripts": true, "tools": true,
}

// ProjectMap walks the tree under root and returns the structural summary.
// The caller must have validated root against the allowed roots.
func (e *UltraFastEngine) ProjectMap(ctx context.Context, root string, opts ProjectMapOpts) (ProjectMapResult, error) {
	if opts.Depth <= 0 {
		opts.Depth = ProjectMapDefaultDepth
	}
	if opts.Depth > ProjectMapMaxDepth {
		opts.Depth = ProjectMapMaxDepth
	}
	if opts.Detail == "" {
		opts.Detail = "normal"
	}
	if opts.MaxEntries <= 0 {
		opts.MaxEntries = ProjectMapMaxEntries
		if e.GetConfig() != nil && e.GetConfig().ProjectMapMaxEntries > 0 {
			opts.MaxEntries = e.GetConfig().ProjectMapMaxEntries
		}
	}
	if opts.MaxTime <= 0 {
		opts.MaxTime = ProjectMapMaxTime
		if e.GetConfig() != nil && e.GetConfig().ProjectMapMaxTime > 0 {
			opts.MaxTime = e.GetConfig().ProjectMapMaxTime
		}
	}
	if opts.MaxBytesMetadata <= 0 {
		opts.MaxBytesMetadata = ProjectMapMaxMetadataBytes
	}

	w := &projectMapWalk{
		engine:   e,
		opts:     opts,
		root:     root,
		ignore:   NewIgnoreMatcher(),
		start:    time.Now(),
		deadline: time.Now().Add(opts.MaxTime),
		byExt:    map[string]int{},
	}
	rootNode := w.walkDir(ctx, root, 0)
	w.tree = rootNode
	if w.skippedIgnore > 0 {
		w.warn(fmt.Sprintf("%d entries skipped by ignore rules", w.skippedIgnore))
	}

	res := ProjectMapResult{
		Roots:        e.ListedAllowedPaths(),
		Truncated:    w.truncated,
		SummaryScope: ProjectMapSummaryScope,
		Limits: ProjectMapLimits{
			MaxEntries:       opts.MaxEntries,
			MaxDepth:         opts.Depth,
			MaxTimeMS:        opts.MaxTime.Milliseconds(),
			MaxBytesMetadata: opts.MaxBytesMetadata,
			ElapsedMS:        time.Since(w.start).Milliseconds(),
			VisitedEntries:   w.entries,
		},
		Summary: ProjectMapSummary{
			VisitedFiles: w.files,
			VisitedDirs:  w.dirs,
			ByExtension:  w.byExt,
			LargestFiles: SortProjectMapLargest(w.largest, ProjectMapMaxLargestFiles),
			KeyFiles:     w.key,
		},
		Structure: filterProjectMapStructure(flattenProjectMap(w.tree), opts.Detail),
		Warnings:  w.warnings,
	}
	if res.Roots == nil {
		res.Roots = []string{}
	}
	if res.Summary.ByExtension == nil {
		res.Summary.ByExtension = map[string]int{}
	}
	if res.Summary.LargestFiles == nil {
		res.Summary.LargestFiles = []ProjectMapFile{}
	}
	if res.Summary.KeyFiles == nil {
		res.Summary.KeyFiles = []string{}
	}
	if res.Structure == nil {
		res.Structure = []ProjectMapEntry{}
	}
	if res.Warnings == nil {
		res.Warnings = []string{}
	}
	return res, nil
}

// walkDir visits one directory and returns the node describing it. level is
// the number of levels below the root (the root itself is level 0). The node
// carries the number of visible children and the notable children.
func (w *projectMapWalk) walkDir(ctx context.Context, dirPath string, level int) *pmNode {
	node := &pmNode{entry: ProjectMapEntry{Path: w.rel(dirPath), Type: "dir"}}
	if w.stop(ctx) {
		return node
	}
	entries, err := w.engine.readDirWithinRoot(dirPath)
	if err != nil {
		// An unreadable directory is not fatal for an orientation call: the
		// rest of the tree is still worth describing.
		w.warn("some directories could not be read")
		return node
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })

	for _, entry := range entries {
		if w.stop(ctx) {
			return node
		}
		child := filepath.Join(dirPath, entry.Name())

		// Symlinks are never followed: a link may point outside the root and
		// a link loop would make the walk unbounded.
		if entry.Type()&os.ModeSymlink != 0 {
			continue
		}

		isDir := entry.IsDir()
		prot := w.engine.ProtectionLevel(child)
		if prot >= LevelHidden {
			// Hidden paths never appear: no name, no size, no count.
			continue
		}
		if skipWalkDir(entry.Name(), child, w.root, isDir, w.ignore, false) {
			// Same exclusions as list_directory / directory_tree / search_files:
			// gitignore-style rules, secret paths and the usual build dirs.
			w.skippedIgnore++
			continue
		}

		rel := w.rel(child)
		if !w.charge(rel) {
			return node
		}
		w.entries++

		if isDir {
			w.dirs++
			var childNode *pmNode
			if level+1 <= w.opts.Depth {
				childNode = w.walkDir(ctx, child, level+1)
			} else {
				// Boundary directory: count its visible children without
				// descending into it.
				childNode = &pmNode{entry: ProjectMapEntry{Path: rel, Type: "dir"}}
				childNode.entry.Children = w.countVisible(child)
			}
			childNode.entry.Path = rel + "/"
			if childNode.entry.Children == 0 {
				childNode.entry.Children = childNode.childrenCount()
			}
			childNode.entry.Notable = childNode.notableChildren()
			node.children = append(node.children, childNode)
			continue
		}

		w.files++
		size := int64(0)
		if prot < LevelProtected {
			if info, err := entry.Info(); err == nil {
				size = info.Size()
			}
			w.byExt[extensionOf(entry.Name())]++
			w.trackLargest(rel, size)
			w.trackKey(rel, entry.Name())
		}
		node.children = append(node.children, &pmNode{entry: ProjectMapEntry{Path: rel, Type: "file"}})
	}
	return node
}

// childrenCount returns how many visible entries the node holds.
func (n *pmNode) childrenCount() int {
	return len(n.children)
}

// countVisible returns how many visible entries a directory holds without
// descending into it. Used for directories at the depth boundary so their
// child count is still accurate.
func (w *projectMapWalk) countVisible(dirPath string) int {
	entries, err := w.engine.readDirWithinRoot(dirPath)
	if err != nil {
		return 0
	}
	n := 0
	for _, entry := range entries {
		if entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		child := filepath.Join(dirPath, entry.Name())
		if w.engine.ProtectionLevel(child) >= LevelHidden {
			continue
		}
		if skipWalkDir(entry.Name(), child, w.root, entry.IsDir(), w.ignore, false) {
			continue
		}
		n++
	}
	return n
}

// notableChildren lists the children worth mentioning: code containers and
// project-defining files. It never names a hidden path.
func (n *pmNode) notableChildren() []string {
	var out []string
	for _, child := range n.children {
		name := child.entry.Path
		if child.entry.Type == "dir" {
			base := strings.TrimSuffix(name, "/")
			base = filepath.Base(base)
			if codeContainerNames[strings.ToLower(base)] || child.holdsSource() {
				out = append(out, name)
			}
			continue
		}
		base := strings.ToLower(filepath.Base(name))
		if keyFileNames[base] {
			out = append(out, name)
			continue
		}
		for _, suffix := range keyFileSuffixes {
			if strings.HasSuffix(base, suffix) {
				out = append(out, name)
				break
			}
		}
	}
	if len(out) > ProjectMapMaxNotable {
		out = out[:ProjectMapMaxNotable]
	}
	return out
}

// holdsSource reports whether the node directly contains a source file.
func (n *pmNode) holdsSource() bool {
	for _, child := range n.children {
		if child.entry.Type != "file" {
			continue
		}
		if sourceExtensions[strings.ToLower(filepath.Ext(child.entry.Path))] {
			return true
		}
	}
	return false
}

// flattenProjectMap renders the tree as a pre-order list.
func flattenProjectMap(node *pmNode) []ProjectMapEntry {
	out := []ProjectMapEntry{}
	if node == nil {
		return out
	}
	var walk func(n *pmNode)
	walk = func(n *pmNode) {
		for _, child := range n.children {
			out = append(out, child.entry)
			walk(child)
		}
	}
	walk(node)
	return out
}

// filterProjectMapStructure applies the detail contract to the skeleton:
//
//	summary — the top level only (directories with their child counts)
//	normal  — every directory, no files (the cheap orientation)
//	full    — every directory and every file
func filterProjectMapStructure(entries []ProjectMapEntry, detail string) []ProjectMapEntry {
	out := []ProjectMapEntry{}
	for _, e := range entries {
		isDir := e.Type == "dir"
		switch detail {
		case "summary":
			if !isDir || projectMapEntryLevel(e.Path) != 1 {
				continue
			}
		case "normal":
			if !isDir {
				continue
			}
		}
		out = append(out, e)
	}
	return out
}

// projectMapEntryLevel returns how deep an entry sits below the root
// (1 = direct child of the root).
func projectMapEntryLevel(path string) int {
	trimmed := strings.TrimSuffix(path, "/")
	if trimmed == "" {
		return 0
	}
	return strings.Count(trimmed, "/") + 1
}

// charge accounts one entry against the metadata budget. It returns false
// when the budget is exhausted (the walk must stop).
func (w *projectMapWalk) charge(path string) bool {
	cost := int64(len(path)) + 64
	if w.metaBytes+cost > w.opts.MaxBytesMetadata {
		w.truncated = true
		w.warn("metadata budget exhausted")
		return false
	}
	w.metaBytes += cost
	return true
}

// warn records a warning once.
func (w *projectMapWalk) warn(msg string) {
	for _, existing := range w.warnings {
		if existing == msg {
			return
		}
	}
	w.warnings = append(w.warnings, msg)
}

// stop reports whether the walk must end: entries cap, deadline, or a
// cancelled context.
func (w *projectMapWalk) stop(ctx context.Context) bool {
	if w.entries >= w.opts.MaxEntries {
		w.truncated = true
		w.warn("entry limit reached")
		return true
	}
	if time.Now().After(w.deadline) {
		w.truncated = true
		w.warn("time limit reached")
		return true
	}
	if ctx.Err() != nil {
		w.truncated = true
		w.warn("cancelled")
		return true
	}
	return false
}

// trackLargest keeps the biggest files seen so far.
func (w *projectMapWalk) trackLargest(rel string, size int64) {
	if size <= 0 {
		return
	}
	w.largest = append(w.largest, ProjectMapFile{Path: rel, Size: size})
}

// trackKey records project-defining files.
func (w *projectMapWalk) trackKey(rel, name string) {
	lower := strings.ToLower(name)
	if keyFileNames[lower] {
		w.key = append(w.key, rel)
		return
	}
	for _, suffix := range keyFileSuffixes {
		if strings.HasSuffix(lower, suffix) {
			w.key = append(w.key, rel)
			return
		}
	}
	for _, marker := range keyFileMarkers {
		if strings.Contains(strings.ToLower(rel), marker) {
			w.key = append(w.key, rel)
			return
		}
	}
}

// rel returns the path of child relative to the mapped root, slash-separated.
func (w *projectMapWalk) rel(child string) string {
	rel, err := filepath.Rel(w.root, child)
	if err != nil {
		return filepath.Base(child)
	}
	rel = filepath.ToSlash(rel)
	if rel == "." {
		return ""
	}
	return rel
}

// extensionOf returns the lowercase extension of a file name ("" when none).
func extensionOf(name string) string {
	ext := filepath.Ext(name)
	if ext == "" {
		return ""
	}
	return strings.ToLower(ext)
}

// SortProjectMapLargest orders the largest_files list by descending size
// (ties broken by path) and caps it.
func SortProjectMapLargest(files []ProjectMapFile, limit int) []ProjectMapFile {
	sort.SliceStable(files, func(i, j int) bool {
		if files[i].Size != files[j].Size {
			return files[i].Size > files[j].Size
		}
		return files[i].Path < files[j].Path
	})
	if limit > 0 && len(files) > limit {
		files = files[:limit]
	}
	return files
}
