package core

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type MultiFilePatchOpts struct {
	BaseDir      string
	DryRun       bool
	AllowRewrite bool
	CreateBackup bool
}

type MultiFilePatchFileResult struct {
	Path     string
	Added    int
	Removed  int
	OldHash  string
	NewHash  string
	BackupID string
}

type MultiFilePatchResult struct {
	Files []MultiFilePatchFileResult
}

type RewriteBlockedError struct {
	Path           string
	Message        string
	OldLen, NewLen int
}

func (e *RewriteBlockedError) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

func ResolvePatchDest(baseDir, header string) (string, error) {
	rel := PatchHeaderPath(header)
	if rel == "" || rel == "/dev/null" || rel == "dev/null" {
		return "", &PatchError{Reason: PatchReasonMalformed, Msg: "patch header has no destination path"}
	}
	if filepath.IsAbs(rel) {
		return "", &PatchError{Reason: PatchReasonMalformed, Msg: "absolute patch headers are not allowed"}
	}
	base := filepath.Clean(baseDir)
	dest := filepath.Join(base, filepath.FromSlash(rel))
	dest = filepath.Clean(dest)
	relBack, err := filepath.Rel(base, dest)
	if err != nil || relBack == ".." || strings.HasPrefix(relBack, ".."+string(os.PathSeparator)) {
		return "", &PatchError{Reason: PatchReasonMalformed, Msg: "patch path escapes base directory"}
	}
	return dest, nil
}

func patchFileHeader(p ParsedPatch) string {
	n := PatchHeaderPath(p.NewFile)
	if n != "" && n != "/dev/null" && n != "dev/null" {
		return p.NewFile
	}
	return p.OldFile
}

func (e *UltraFastEngine) ApplyMultiFilePatch(ctx context.Context, files []ParsedPatch, opts MultiFilePatchOpts) (*MultiFilePatchResult, error) {
	if len(files) < 2 {
		return nil, &PatchError{Reason: PatchReasonMalformed, Msg: "multi-file patch requires at least two files"}
	}
	base := NormalizePath(opts.BaseDir)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !e.IsPathAllowed(base) {
		return nil, e.AccessDeniedError("mutate", base)
	}
	info, err := os.Lstat(base)
	if err != nil {
		return nil, &PatchError{Reason: PatchReasonMalformed, Msg: "multi-file patch requires path to be a directory"}
	}
	if info.Mode()&os.ModeSymlink != 0 {
		resolved, rerr := e.ResolveAndAuthorize("mutate", base)
		if rerr != nil {
			return nil, rerr
		}
		base = resolved
		info, err = os.Lstat(base)
		if err != nil {
			return nil, &PatchError{Reason: PatchReasonMalformed, Msg: "multi-file patch requires path to be a directory"}
		}
	}
	if !info.IsDir() {
		return nil, &PatchError{Reason: PatchReasonMalformed, Msg: "multi-file patch requires path to be a directory"}
	}

	type prepared struct {
		dest     string
		parsed   *ParsedPatch
		newBytes []byte
		added    int
		removed  int
		oldHash  string
	}

	dests := make([]string, 0, len(files))
	seen := make(map[string]struct{}, len(files))
	plans := make([]prepared, 0, len(files))
	for i := range files {
		dest, err := ResolvePatchDest(base, patchFileHeader(files[i]))
		if err != nil {
			return nil, err
		}
		dest = NormalizePath(dest)
		if !e.IsPathAllowed(dest) {
			return nil, e.AccessDeniedError("mutate", dest)
		}
		st, err := os.Lstat(dest)
		if err != nil {
			if os.IsNotExist(err) {
				return nil, &PatchError{Reason: PatchReasonMalformed, Msg: fmt.Sprintf("destination does not exist: %s", dest)}
			}
			return nil, err
		}
		if st.IsDir() {
			return nil, &PatchError{Reason: PatchReasonMalformed, Msg: fmt.Sprintf("cannot patch a directory: %s", dest)}
		}
		if st.Mode()&os.ModeSymlink != 0 {
			resolved, rerr := e.ResolveAndAuthorize("mutate", dest)
			if rerr != nil {
				return nil, rerr
			}
			dest = resolved
			st, err = os.Lstat(dest)
			if err != nil {
				return nil, err
			}
			if st.IsDir() {
				return nil, &PatchError{Reason: PatchReasonMalformed, Msg: fmt.Sprintf("cannot patch a directory: %s", dest)}
			}
		} else if _, rerr := e.ResolveAndAuthorize("mutate", dest); rerr != nil {
			return nil, rerr
		}
		canon := CanonicalPath(dest)
		if _, dup := seen[canon]; dup {
			return nil, &PatchError{Reason: PatchReasonMalformed, Msg: fmt.Sprintf("duplicate path in patch: %s", dest)}
		}
		seen[canon] = struct{}{}
		dests = append(dests, dest)
		plans = append(plans, prepared{dest: dest, parsed: &files[i]})
	}

	ctx, unlock, err := e.coordinateFiles(ctx, dests)
	if err != nil {
		return nil, err
	}
	defer unlock()

	journal := &mutationJournal{}
	ctx = context.WithValue(ctx, journalKey{}, journal)

	var txns []*FileTxn
	defer func() {
		for _, t := range txns {
			t.Release()
		}
	}()

	for i := range plans {
		pctx, txn, err := e.BeginFileTxn(ctx, plans[i].dest, false)
		if err != nil {
			return nil, err
		}
		ctx = pctx
		txns = append(txns, txn)
		snap := txn.Snapshot()
		if !snap.Exists {
			return nil, &PatchError{Reason: PatchReasonMalformed, Msg: fmt.Sprintf("destination does not exist: %s", plans[i].dest)}
		}
		newContent, err := applyParsedPatch(string(snap.Bytes), plans[i].parsed)
		if err != nil {
			return nil, err
		}
		if sig := CheckEditRewrite(string(snap.Bytes), newContent, int64(len(snap.Bytes))); sig.BlockOp && !opts.AllowRewrite {
			return nil, &RewriteBlockedError{Path: plans[i].dest, Message: sig.Message, OldLen: len(snap.Bytes), NewLen: len(newContent)}
		}
		added, removed, _ := DiffCounts(string(snap.Bytes), newContent)
		plans[i].newBytes = []byte(newContent)
		plans[i].added = added
		plans[i].removed = removed
		plans[i].oldHash = snap.Hash
	}

	out := &MultiFilePatchResult{Files: make([]MultiFilePatchFileResult, len(plans))}
	for i, p := range plans {
		out.Files[i] = MultiFilePatchFileResult{
			Path:    p.dest,
			Added:   p.added,
			Removed: p.removed,
			OldHash: p.oldHash,
			NewHash: contentHashFNV(string(p.newBytes)),
		}
	}
	if opts.DryRun {
		return out, nil
	}

	for i, p := range plans {
		if !opts.CreateBackup {
			txns[i].SkipBackup()
		}
		backupID, err := txns[i].Commit(ctx, p.newBytes, false, "apply_patch", "")
		if err != nil {
			journal.rollback(ctx, e)
			return nil, err
		}
		out.Files[i].BackupID = backupID
	}
	return out, nil
}
