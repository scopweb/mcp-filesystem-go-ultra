# Hooks System Documentation

## Overview

The MCP Filesystem Ultra-Fast server includes a powerful hooks system inspired by Claude Code, allowing you to execute custom commands before and after file operations. This enables automatic code formatting, validation, linting, and custom workflows.

## Features

- **12 Hook Events**: Pre/post hooks for write, edit, delete, create, move, and copy operations
- **Pattern Matching**: Target specific files using exact matches, wildcards, or patterns
- **Parallel Execution**: Hooks run concurrently with automatic deduplication
- **Content Modification**: Hooks can modify content (e.g., auto-format code before writing)
- **Flexible Output**: Support for both simple exit codes and advanced JSON responses
- **Timeout Control**: Configurable timeouts per hook (default: 60 seconds)
- **Error Handling**: Choose whether operations should fail if hooks fail

## Hook Events

### File Writing
- **`pre-write`**: Executes before writing a file (can format/validate content)
- **`post-write`**: Executes after successfully writing a file

### File Editing
- **`pre-edit`**: Executes before editing a file (can validate changes)
- **`post-edit`**: Executes after successfully editing a file (can verify compilation)

### File Deletion
- **`pre-delete`**: Executes before deleting a file/directory (can prevent deletion)
- **`post-delete`**: Executes after successfully deleting

### Directory Creation
- **`pre-create`**: Executes before creating a directory
- **`post-create`**: Executes after successfully creating a directory

### File Moving
- **`pre-move`**: Executes before moving a file/directory
- **`post-move`**: Executes after successfully moving

### File Copying
- **`pre-copy`**: Executes before copying a file/directory
- **`post-copy`**: Executes after successfully copying

## Configuration

### Enable Hooks

Start the server with hooks enabled:

```bash
mcp-filesystem-ultra.exe --hooks-enabled --hooks-config=hooks.json
```

### Configuration File Format

Create a `hooks.json` file:

```json
{
  "hooks": {
    "pre-write": [
      {
        "pattern": "*.go",
        "hooks": [
          {
            "type": "command",
            "command": "gofmt -w",
            "timeout": 10,
            "failOnError": false,
            "description": "Format Go files before writing",
            "enabled": true
          }
        ]
      }
    ],
    "post-edit": [
      {
        "pattern": "*.go",
        "hooks": [
          {
            "type": "command",
            "command": "go build",
            "timeout": 60,
            "failOnError": false,
            "description": "Verify Go files compile after edit",
            "enabled": true
          }
        ]
      }
    ]
  }
}
```

## Hook Configuration Options

### Matcher

```json
{
  "pattern": "*.go",  // Pattern to match files
  "hooks": [...]      // Array of hooks to execute
}
```

**Pattern Types:**
- **Exact**: `"main.go"` - Matches exactly "main.go"
- **Wildcard**: `"*.go"` - Matches all .go files
- **Wildcard**: `"test_*"` - Matches files starting with "test_"
- **Universal**: `"*"` - Matches all files

### Hook Object

```json
{
  "type": "command",              // "command" or "script"
  "command": "gofmt -w",          // Command to execute
  "script": "/path/to/script.sh", // Script path (if type=script)
  "timeout": 10,                  // Timeout in seconds (default: 60)
  "failOnError": false,           // Fail operation if hook fails
  "description": "Format code",   // Human-readable description
  "enabled": true                 // Enable/disable this hook
}
```

## Hook Input

Hooks receive a JSON object via **stdin** with the following structure:

```json
{
  "event": "pre-write",
  "tool_name": "write_file",
  "file_path": "C:\\project\\main.go",
  "operation": "write",
  "content": "package main...",
  "old_content": "...",
  "new_content": "...",
  "source_path": "...",
  "dest_path": "...",
  "timestamp": "2025-10-24T10:30:00Z",
  "working_dir": "C:\\project",
  "metadata": {}
}
```

## Hook Output

### Simple Mode (Exit Codes)

Return exit codes to indicate success or failure:

- **`0`**: Success - Operation proceeds, stdout shown in logs
- **`2`**: Block operation - stderr message fed to Claude
- **Other**: Non-blocking error - logged but operation proceeds

Example bash script:

```bash
#!/bin/bash
# Read JSON from stdin
input=$(cat)

# Extract file path
file_path=$(echo "$input" | jq -r '.file_path')

# Run formatter
if gofmt -w "$file_path"; then
  exit 0  # Success
else
  echo "Formatting failed" >&2
  exit 2  # Block operation
fi
```

### Advanced Mode (JSON Output)

Return structured JSON via stdout for fine-grained control:

```json
{
  "decision": "allow",
  "reason": "File formatted successfully",
  "modified_content": "package main\n\nfunc main() {\n\tfmt.Println(\"formatted\")\n}",
  "additional_context": "Applied gofmt formatting",
  "metadata": {
    "formatter": "gofmt",
    "lines_changed": 3
  }
}
```

**Decision Values:**
- **`allow`**: Allow the operation to proceed
- **`deny`**: Deny the operation (with reason)
- **`continue`**: Continue with next hook

**Response Fields:**
- `decision` (string): The decision (allow/deny/continue)
- `reason` (string): Explanation for the decision
- `modified_content` (string): Modified file content (for pre-write/pre-edit hooks)
- `additional_context` (string): Additional context to log
- `metadata` (object): Custom metadata

Example Python script:

```python
#!/usr/bin/env python3
import json
import sys
import subprocess

# Read hook context from stdin
context = json.load(sys.stdin)
file_path = context['file_path']
content = context.get('content', '')

# Run black formatter
try:
    result = subprocess.run(
        ['black', '--quiet', '-'],
        input=content.encode(),
        capture_output=True,
        check=True
    )
    formatted_content = result.stdout.decode()

    # Return formatted content
    response = {
        "decision": "allow",
        "modified_content": formatted_content,
        "reason": "Code formatted with black"
    }
    print(json.dumps(response))
    sys.exit(0)

except subprocess.CalledProcessError:
    # Formatting failed - deny operation
    response = {
        "decision": "deny",
        "reason": "Black formatter failed - code may have syntax errors"
    }
    print(json.dumps(response))
    sys.exit(2)
```

## Common Use Cases

### 1. Auto-Format Code Before Writing

```json
{
  "hooks": {
    "pre-write": [
      {
        "pattern": "*.go",
        "hooks": [{
          "command": "gofmt -w",
          "failOnError": false,
          "enabled": true
        }]
      },
      {
        "pattern": "*.js",
        "hooks": [{
          "command": "prettier --write",
          "failOnError": false,
          "enabled": true
        }]
      },
      {
        "pattern": "*.py",
        "hooks": [{
          "command": "black",
          "failOnError": false,
          "enabled": true
        }]
      }
    ]
  }
}
```

### 2. Validate Code After Editing

```json
{
  "hooks": {
    "post-edit": [
      {
        "pattern": "*.go",
        "hooks": [{
          "command": "go vet",
          "timeout": 30,
          "failOnError": false,
          "enabled": true
        }]
      }
    ]
  }
}
```

### 3. Run Tests Before Committing

```json
{
  "hooks": {
    "pre-write": [
      {
        "pattern": "*_test.go",
        "hooks": [{
          "command": "go test -v",
          "timeout": 120,
          "failOnError": true,
          "enabled": true
        }]
      }
    ]
  }
}
```

### 4. Prevent Deletion of Important Files

```json
{
  "hooks": {
    "pre-delete": [
      {
        "pattern": "*.env",
        "hooks": [{
          "command": "echo 'Cannot delete .env files' && exit 2",
          "failOnError": true,
          "enabled": true
        }]
      }
    ]
  }
}
```

### 5. Backup Before Editing

```json
{
  "hooks": {
    "pre-edit": [
      {
        "pattern": "*",
        "hooks": [{
          "command": "cp \"$FILE_PATH\" \"$FILE_PATH.backup\"",
          "failOnError": false,
          "enabled": true
        }]
      }
    ]
  }
}
```

## Hook Execution Details

### Parallel Execution

- Multiple hooks for the same event run concurrently
- Hooks with identical commands are deduplicated (run only once)
- All hooks must complete before the operation proceeds

### Timeout Handling

- Default timeout: 60 seconds
- Configurable per hook
- If a hook times out, it's treated as a non-blocking error (unless `failOnError: true`)

### Error Handling

- **`failOnError: false`** (default): Errors are logged but don't block the operation
- **`failOnError: true`**: Hook failures cancel the operation

### Content Modification

For `pre-write` and `pre-edit` hooks:
- If a hook returns `modified_content`, that content is used instead of the original
- Multiple hooks can modify content sequentially
- The final modified content is what gets written

## Security Considerations

⚠️ **IMPORTANT**: Hooks execute arbitrary commands on your system. Follow these security best practices:

1. **Validate Inputs**: Always validate file paths and content in your hook scripts
2. **Quote Variables**: Use proper quoting in shell commands to prevent injection
3. **Use Absolute Paths**: Specify full paths for scripts and executables
4. **Restrict Patterns**: Use specific patterns instead of wildcards when possible
5. **Review Configuration**: Audit your hooks.json before enabling hooks
6. **Sandbox Scripts**: Consider running hooks in restricted environments
7. **Avoid Sensitive Files**: Don't create hooks that expose `.env`, keys, or credentials

### Example: Safe Hook Script

```bash
#!/bin/bash
set -euo pipefail  # Exit on error, undefined variables, pipe failures

# Read and validate input
input=$(cat)
file_path=$(echo "$input" | jq -r '.file_path')

# Validate file path (prevent path traversal)
if [[ "$file_path" =~ \.\. ]]; then
  echo "Invalid file path" >&2
  exit 2
fi

# Process file safely
# ... your logic here ...
```

## Debugging Hooks

Enable debug mode to see hook execution details:

```bash
mcp-filesystem-ultra.exe --hooks-enabled --hooks-config=hooks.json --debug
```

Debug output includes:
- Hook execution start/end
- Hook stdout/stderr
- Execution duration
- Decision results

## Performance Considerations

- Hooks add latency to file operations
- Use timeouts to prevent hanging operations
- Consider disabling hooks in performance-critical scenarios
- Hooks run in parallel but overall operation waits for all hooks

## Troubleshooting

### Hook Not Executing

1. Check pattern matches the file: `"*.go"` for Go files
2. Verify `enabled: true` in hook configuration
3. Ensure hooks are enabled: `--hooks-enabled`
4. Check hook configuration path: `--hooks-config=hooks.json`

### Hook Failing

1. Test command manually in terminal
2. Check timeout is sufficient
3. Verify command exists in PATH
4. Review stderr output in debug mode
5. Check file permissions

### Content Not Modified

1. Ensure hook returns `modified_content` in JSON output
2. Check hook decision is `allow` not `continue`
3. Verify hook runs before write/edit (use `pre-write`/`pre-edit`)

## Examples

See [hooks.example.json](hooks.example.json) for a complete configuration example with:
- Go formatting (gofmt)
- JavaScript formatting (prettier)
- Python formatting (black)
- Go validation (go vet)
- Build verification (go build)

## API Reference

### HookContext Structure

```go
type HookContext struct {
    Event         string                 // Hook event name
    ToolName      string                 // MCP tool being executed
    FilePath      string                 // File being operated on
    Operation     string                 // Operation type
    Content       string                 // File content (for write/edit)
    OldContent    string                 // Previous content (for edit)
    NewContent    string                 // New content (for edit)
    SourcePath    string                 // Source path (for move/copy)
    DestPath      string                 // Destination path (for move/copy)
    Timestamp     time.Time              // Operation timestamp
    WorkingDir    string                 // Current working directory
    Metadata      map[string]interface{} // Additional metadata
}
```

### HookResult Structure

```go
type HookResult struct {
    Decision          string                 // allow, deny, or continue
    Reason            string                 // Reason for decision
    ModifiedContent   string                 // Modified file content
    AdditionalContext string                 // Additional context
    Metadata          map[string]interface{} // Custom metadata
    Stdout            string                 // Command stdout
    Stderr            string                 // Command stderr
    ExitCode          int                    // Command exit code
    Duration          time.Duration          // Execution duration
}
```

## Conclusion

The hooks system provides powerful extensibility for the MCP Filesystem server, enabling automatic code formatting, validation, and custom workflows that integrate seamlessly with Claude Desktop.

For questions or issues, please refer to the main [README.md](README.md) or file an issue on GitHub.
