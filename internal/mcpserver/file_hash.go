package mcpserver

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash"
	"io"
	"os"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mcp/filesystem-ultra/core"
)

type inspectHashFile struct {
	file   *os.File
	path   string
	before os.FileInfo
	digest hash.Hash
	size   int64
}

func inspectFileHash(ctx context.Context, engine *core.UltraFastEngine, args map[string]any) *mcp.CallToolResult {
	ctx, cancel := context.WithTimeout(ctx, core.DefaultOperationTimeout)
	defer cancel()
	detail, err := inspectEnum(args, "detail", "normal", "summary", "normal", "full")
	if err != nil {
		return validationResult(err.Error(), "detail", "summary|normal|full")
	}
	paths, err := inspectStrings(args, "paths", inspectHashPaths)
	if err != nil {
		return validationResult(err.Error(), "paths", "1..64 paths")
	}
	path, hasPath := args["path"]
	_, hasPaths := args["paths"]
	if hasPath == hasPaths {
		return validationResult("provide exactly one of path or paths", "path", "path XOR paths")
	}
	if hasPath {
		s, ok := path.(string)
		if !ok || s == "" {
			return validationResult("path must be non-empty", "path", "string")
		}
		paths = []string{s}
	}
	compare, _ := args["compare"].(bool)
	if compare && (!hasPaths || len(paths) != 2) {
		return validationResult("compare:true requires exactly two paths", "paths", "two paths")
	}
	files := make([]inspectHashFile, 0, len(paths))
	defer func() {
		for _, item := range files {
			item.file.Close()
		}
	}()
	// Authorize/open the whole batch before hashing; no partial success response.
	for _, path := range paths {
		f, canonical, denied := inspectOpen(ctx, engine, path)
		if denied != nil {
			return denied
		}
		before, err := f.Stat()
		if err != nil {
			f.Close()
			return inspectIOError(path, err)
		}
		files = append(files, inspectHashFile{file: f, path: canonical, before: before, digest: sha256.New()})
	}
	var difference map[string]any
	if compare {
		var offset int64
		buffers := [2][]byte{make([]byte, core.DefaultBufferSize), make([]byte, core.DefaultBufferSize)}
		for {
			var counts [2]int
			for i := 0; i < 2; i++ {
				n, err := io.ReadFull(inspectContextReader{ctx, files[i].file}, buffers[i])
				counts[i] = n
				if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
					return inspectIOError(files[i].path, err)
				}
				files[i].digest.Write(buffers[i][:n])
				files[i].size += int64(n)
			}
			if counts[0] == 0 && counts[1] == 0 {
				break
			}
			if difference == nil {
				for i := 0; i < max(counts[0], counts[1]); i++ {
					if i >= counts[0] || i >= counts[1] || buffers[0][i] != buffers[1][i] {
						var a, b any
						if i < counts[0] {
							a = int(buffers[0][i])
						}
						if i < counts[1] {
							b = int(buffers[1][i])
						}
						difference = map[string]any{"offset": offset + int64(i), "a": a, "b": b}
						break
					}
				}
			}
			offset += int64(max(counts[0], counts[1]))
		}
	} else {
		buffer := make([]byte, core.DefaultBufferSize)
		for i := range files {
			n, err := io.CopyBuffer(files[i].digest, inspectContextReader{ctx, files[i].file}, buffer)
			if err != nil {
				return inspectIOError(files[i].path, err)
			}
			files[i].size = n
		}
	}
	hashes := make([]map[string]any, 0, len(files))
	for _, item := range files {
		if err := ctx.Err(); err != nil {
			return inspectFailure("CANCELLED", item.path, err.Error())
		}
		if err := inspectStable(item.file, item.before, item.path); err != nil {
			return inspectFailure("FILE_CHANGED", item.path, err.Error())
		}
		if item.size != item.before.Size() {
			return inspectFailure("FILE_CHANGED", item.path, "observed size differs from initial size")
		}
		hashes = append(hashes, map[string]any{"path": item.path, "algorithm": "sha256", "hash": hex.EncodeToString(item.digest.Sum(nil)), "size": item.size, "mtime": item.before.ModTime().UTC().Format(time.RFC3339Nano)})
	}
	payload := map[string]any{"status": "success", "hashes": hashes, "truncated": false}
	if compare {
		payload["identical"] = difference == nil
		if difference != nil {
			payload["first_difference"] = difference
		}
	}
	summary := fmt.Sprintf("file_hash | %d files | SHA-256", len(files))
	if compare {
		summary += fmt.Sprintf(" | identical:%v", difference == nil)
	}
	body := ""
	for _, h := range hashes {
		body += fmt.Sprintf("%s  %s  (%d bytes)\n", h["hash"], h["path"], h["size"])
	}
	if compare {
		body += fmt.Sprintf("identical: %v", difference == nil)
		if difference != nil {
			body += fmt.Sprintf(" | first difference: byte %v (a=%v, b=%v)", difference["offset"], difference["a"], difference["b"])
		}
	}
	result, fits := inspectResponse(engine, payload, body, summary, detail)
	if !fits {
		return inspectFailure("LIMIT_EXCEEDED", "", "hash batch exceeds response size; use fewer paths")
	}
	return result
}
