package core

// ActionClass is the E4.1 mutating/read classification for a tool.
type ActionClass int

const (
	ClassRead ActionClass = iota
	ClassWrite
	ClassMixed
)

// ParamSpec is one field in the unified tool contract.
type ParamSpec struct {
	Type     ParamType
	Required bool
	Enum     []string
}

// ToolContract is the single source for validation, enums, examples, and
// action classification. MCP registration in tools_*.go stays as the wire
// schema; tests assert the registered properties are a subset of Params.
type ToolContract struct {
	Name     string
	Class    ActionClass
	Params   map[string]ParamSpec
	Examples []string
	AnyOf    [][]string
	Mutating func(map[string]interface{}) bool
}

var contracts map[string]*ToolContract

func init() {
	contracts = buildContracts()
}

// Contract returns the E4.1 contract for a tool.
func Contract(name string) (*ToolContract, bool) {
	c, ok := contracts[name]
	return c, ok
}

// ContractExamples returns curated help examples (may be empty).
func ContractExamples(name string) []string {
	if c, ok := contracts[name]; ok {
		return append([]string(nil), c.Examples...)
	}
	return nil
}

// IsMutating reports whether the call mutates the host filesystem.
func IsMutating(name string, args map[string]interface{}) bool {
	c, ok := contracts[name]
	if !ok {
		return false
	}
	switch c.Class {
	case ClassWrite:
		return true
	case ClassRead:
		return false
	default:
		if c.Mutating != nil {
			return c.Mutating(args)
		}
		return true
	}
}

func buildContracts() map[string]*ToolContract {
	out := make(map[string]*ToolContract, len(toolSchemas))
	for name, schema := range toolSchemas {
		params := make(map[string]ParamSpec, len(schema))
		for k, d := range schema {
			params[k] = ParamSpec{Type: d.Type, Required: d.Required}
		}
		out[name] = &ToolContract{Name: name, Params: params, Class: classFor(name)}
	}
	applyContractOverlays(out)
	return out
}

func classFor(name string) ActionClass {
	switch name {
	case "write_file", "edit_file", "multi_edit", "delete_file", "move_file",
		"copy_file", "create_directory", "batch_operations", "project_replace",
		"minify_js", "apply_patch":
		return ClassWrite
	case "wsl", "git", "backup", "server_info":
		return ClassMixed
	default:
		return ClassRead
	}
}

func applyContractOverlays(cs map[string]*ToolContract) {
	setEnum := func(tool, param string, enum ...string) {
		c := cs[tool]
		if c == nil {
			return
		}
		p := c.Params[param]
		p.Enum = enum
		c.Params[param] = p
	}
	setEnum("read_file", "mode", "all", "head", "tail")
	setEnum("read_file", "encoding", "utf-8", "utf8", "base64")
	setEnum("write_file", "mode", "overwrite", "append")
	setEnum("write_file", "encoding", "utf-8", "utf8", "base64")
	setEnum("edit_file", "mode", "replace", "search_replace", "regex", "delete_range", "replace_range", "insert")
	setEnum("edit_file", "diff_format", "auto", "full", "summary", "stat", "none")
	setEnum("edit_file", "position", "after", "before")
	setEnum("multi_edit", "diff_format", "auto", "full", "summary", "stat", "none")
	setEnum("git", "action", "status", "diff", "log", "show", "add", "commit", "push", "fetch", "restore", "branch", "init")
	setEnum("backup", "action", "list", "info", "compare", "cleanup", "restore", "undo_last", "undo_chain", "list_trash", "restore_trash", "purge_trash")
	setEnum("analyze_operation", "operation", "file", "optimize", "write", "edit", "delete")
	setEnum("wsl", "action", "sync", "status", "autosync_config", "autosync_status")
	setEnum("server_info", "action", "help", "stats", "artifact")

	if c := cs["read_file"]; c != nil {
		c.AnyOf = [][]string{{"path", "paths"}}
		c.Examples = []string{
			`read_file(path:"file.go")`,
			`read_file(paths:["a.go","b.go"])`,
			`read_file(path:"app.log", mode:"tail", max_lines:40)`,
			`read_file(path:"file.bin", encoding:"base64")`,
		}
	}
	if c := cs["write_file"]; c != nil {
		c.Examples = []string{
			`write_file(path:"file.go", content:"package main\n")`,
			`write_file(path:"log.txt", content:"line\n", mode:"append")`,
		}
	}
	if c := cs["edit_file"]; c != nil {
		c.Examples = []string{
			`edit_file(path:"file.go", old_text:"foo", new_text:"bar")`,
			`edit_file(path:"file.go", old_text:"foo", new_text:"bar", expected_matches:1, strict:true)`,
			`edit_file(path:"file.go", mode:"delete_range", start_line:10, line_count:3)`,
		}
	}
	if c := cs["multi_edit"]; c != nil {
		c.AnyOf = [][]string{{"edits", "edits_json"}}
		c.Examples = []string{
			`multi_edit(path:"file.go", edits:[{"old_text":"a","new_text":"b"}])`,
			`multi_edit(path:"file.go", edits_json:'[{"old_text":"a","new_text":"b"}]')`,
		}
	}
	if c := cs["delete_file"]; c != nil {
		c.AnyOf = [][]string{{"path", "paths"}}
	}
	if c := cs["get_file_info"]; c != nil {
		c.AnyOf = [][]string{{"path", "paths"}}
	}
	if c := cs["batch_operations"]; c != nil {
		c.Examples = []string{
			`batch_operations(request={"operations":[{"type":"write","path":"a.txt","content":"x"}],"atomic":true})`,
			`batch_operations(request_json='{"operations":[{"type":"write","path":"a.txt","content":"x"}],"atomic":true}')`,
		}
	}
	if c := cs["git"]; c != nil {
		c.Examples = []string{
			`git(action:"status")`,
			`git(action:"diff", paths:["lib/dPeticiones.php"], output:"stat")`,
			`git(action:"log", limit:5)`,
			`git(action:"show", rev:"HEAD", output:"full")`,
			`git(action:"add", paths:["src/file.php"])`,
			`git(action:"commit", message:"fix: short description")`,
			`git(action:"push")`,
			`git(action:"fetch", prune:true)`,
			`git(action:"restore", paths:["file.txt"], staged:true)`,
			`git(action:"branch", name:"feature/new", checkout:true)`,
			`git(action:"branch", name:"old", delete:true)`,
		}
		c.Mutating = func(args map[string]interface{}) bool {
			a, _ := args["action"].(string)
			switch a {
			case "status", "diff", "log", "show", "":
				return false
			case "branch":
				name, _ := args["name"].(string)
				del, _ := args["delete"].(bool)
				co, _ := args["checkout"].(bool)
				return name != "" || del || co
			default:
				return true
			}
		}
	}
	if c := cs["wsl"]; c != nil {
		c.Mutating = func(args map[string]interface{}) bool {
			a, _ := args["action"].(string)
			switch a {
			case "status", "autosync_status":
				return false
			default:
				return true
			}
		}
	}
	if c := cs["backup"]; c != nil {
		c.Mutating = func(args map[string]interface{}) bool {
			a, _ := args["action"].(string)
			switch a {
			case "restore", "undo_last", "cleanup", "purge_trash", "restore_trash":
				return true
			default:
				return false
			}
		}
	}
	if c := cs["server_info"]; c != nil {
		c.Mutating = func(args map[string]interface{}) bool {
			a, _ := args["action"].(string)
			sub, _ := args["sub_action"].(string)
			return a == "artifact" && sub == "write"
		}
	}
}

func inAnyOf(name string, groups [][]string) bool {
	for _, g := range groups {
		for _, k := range g {
			if k == name {
				return true
			}
		}
	}
	return false
}
