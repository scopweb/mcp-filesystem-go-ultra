package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
)

func gitlabCreate(ctx context.Context, t glTarget, args map[string]interface{}) *mcp.CallToolResult {
	title, set, errRes := stringField(args, "title")
	if errRes != nil {
		return errRes
	}
	if !set || strings.TrimSpace(title) == "" {
		return ghFail(errCodeInvalidParams, `parameter "title" is required`, `pass title for create`, false, map[string]any{"field": "title"})
	}
	if errRes = boundText("title", title, 255); errRes != nil {
		return errRes
	}
	fields := map[string]string{"title": title}
	body, set, errRes := stringField(args, "body")
	if errRes != nil {
		return errRes
	}
	if set {
		if errRes = boundText("body", body, 65536); errRes != nil {
			return errRes
		}
		fields["description"] = body
	}
	labels, set, errRes := stringArrayField(args, "labels")
	if errRes != nil {
		return errRes
	}
	if set {
		fields["labels"] = strings.Join(labels, ",")
	}
	payload, _ := json.Marshal(fields)
	status, _, resp, errRes := gitlabCall(ctx, t, http.MethodPost, "/issues", payload, true)
	if errRes != nil {
		return errRes
	}
	if fail := gitlabStatus(status, resp, true); fail != nil {
		return fail
	}
	var issue map[string]any
	if err := json.Unmarshal(resp, &issue); err != nil || glIID(issue) <= 0 {
		return ghFail(errCodeGLNetwork, "GitLab returned an invalid create response; the issue may have been created", "Check the project before creating again.", false, nil)
	}
	n := glIID(issue)
	out := map[string]any{"status": statusApplied, "action": "create", "repo": t.Project, "base_url": t.Base, "number": n, "url": stringFieldAny(issue["web_url"]), "message": fmt.Sprintf("created #%d", n)}
	return mcp.NewToolResultStructured(out, fmt.Sprintf("created #%d %s\n", n, out["url"]))
}
