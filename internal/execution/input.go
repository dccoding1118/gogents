package execution

import (
	"fmt"
	"strings"

	"github.com/dccoding/gogents/internal/foundation"
	"github.com/dccoding/gogents/internal/source"
)

// basePromptTemplate is the fixed English block that wraps the task content
// (D03 §3.2). The single placeholder `<absolute-task-dir>` is rendered by
// straight string replacement; no conditional logic, no nested templates.
const basePromptTemplate = `You are a coding agent running inside a prepared workspace.

## Project Context
Read ` + "`AGENTS.md`" + ` at the repository root before starting. It describes
the project conventions, architecture, and constraints you must follow.

## Task Workflow
1. Implement the task described in the "Task Content" section below
   on the current git branch.
2. Commit with message format: ` + "`<type>: #<issue-number> <description>`" + `.
   Allowed types: feat, fix, chore, docs, refactor, test.
3. Push the branch: ` + "`git push origin <current-branch>`" + `.
4. Open a PR via ` + "`gh pr create`" + `.
5. When finished (success or unrecoverable failure), write a result
   report to ` + "`<absolute-task-dir>/result.json`" + ` with fields:
   { "status": "success"|"failed", "pr_url": "...", "message": "..." }

If you cannot complete the task, still write result.json with
status=failed and a clear message explaining why.

The following "Task Content" section contains the original issue
as written by the reporter; it may be in any language. Treat its
language as-is, do not translate.

---
`

// renderInputMd builds the input.md content. The output starts with the base
// prompt (with absolute-task-dir substituted), then a separator, then the
// "Task Content" section assembled from the fetched issue (D03 §3.2).
func renderInputMd(content source.TaskContent, absTaskDir string) []byte {
	base := strings.ReplaceAll(basePromptTemplate, "<absolute-task-dir>", absTaskDir)

	var b strings.Builder
	b.WriteString(base)
	b.WriteString("\n# Task Content\n\n")
	fmt.Fprintf(&b, "Source: %s\n", content.URL)
	fmt.Fprintf(&b, "Issue Title: %s\n", content.Title)
	fmt.Fprintf(&b, "Reporter: @%s\n\n", content.Author)
	b.WriteString("## Description\n")
	b.WriteString(content.Body)
	b.WriteString("\n")

	if len(content.Comments) > 0 {
		b.WriteString("\n## Comments\n")
		loc := foundation.Timezone()
		for _, c := range content.Comments {
			fmt.Fprintf(&b, "### @%s at %s\n", c.Author,
				c.CreatedAt.In(loc).Format("2006-01-02T15:04:05-07:00"))
			b.WriteString(c.Body)
			b.WriteString("\n\n")
		}
	}

	return []byte(b.String())
}
