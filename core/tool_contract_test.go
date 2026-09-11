package core

import "testing"

func TestContract_CoversSchemaRegistry(t *testing.T) {
	for name := range toolSchemas {
		if _, ok := Contract(name); !ok {
			t.Errorf("no contract for schema tool %q", name)
		}
	}
}

func TestContract_IsMutating(t *testing.T) {
	if !IsMutating("write_file", nil) {
		t.Fatal("write_file")
	}
	if IsMutating("read_file", nil) {
		t.Fatal("read_file")
	}
	if IsMutating("git", map[string]interface{}{"action": "status"}) {
		t.Fatal("git status")
	}
	if !IsMutating("git", map[string]interface{}{"action": "commit"}) {
		t.Fatal("git commit")
	}
	if IsMutating("git", map[string]interface{}{"action": "branch"}) {
		t.Fatal("git branch list")
	}
	if !IsMutating("server_info", map[string]interface{}{"action": "artifact", "sub_action": "write"}) {
		t.Fatal("artifact write")
	}
}

func TestContract_Examples(t *testing.T) {
	for _, name := range []string{"read_file", "write_file", "edit_file", "multi_edit", "batch_operations", "git"} {
		if len(ContractExamples(name)) == 0 {
			t.Errorf("%s: missing published examples", name)
		}
	}
}

func TestContract_EnumsMatchValidation(t *testing.T) {
	errs := ValidateToolParams("git", map[string]interface{}{"action": "nope"})
	if len(errs) == 0 {
		t.Fatal("invalid git action accepted")
	}
}
