package execution

import (
	"fmt"
	"strings"

	"github.com/dccoding/gogents/internal/foundation"
	"github.com/dccoding/gogents/internal/source"
)

// basePromptTemplate is the fixed English block that wraps the task content
// (D03 §3.2). The placeholders `<absolute-task-dir>` and `<reponame>` are
// rendered by straight string replacement; no conditional logic, no nested
// templates. The agent's working directory is the workspace root, so the repo
// lives in the `./<reponame>` subdirectory.
const basePromptTemplate = `You are a coding agent running inside a prepared workspace.
Your current working directory is the workspace root. The git
repository to work in is the ` + "`./<reponame>`" + ` subdirectory; run all
git and build commands there. Workspace-level configuration
(` + "`.claude/`, `.mcp.json`" + `) applies to you from the working directory.

## Project Context
Read ` + "`AGENTS.md`" + ` at the repository root (` + "`./<reponame>/AGENTS.md`" + `)
before starting. It describes the project conventions, architecture,
and constraints you must follow.

## Task Workflow
1. Change into ` + "`./<reponame>`" + ` and implement the task described in the
   "Task Content" section below on the current git branch.
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
// prompt (with absolute-task-dir and reponame substituted), then a separator,
// then the "Task Content" section assembled from the fetched issue (D03 §3.2).
func renderInputMd(content source.TaskContent, absTaskDir, reponame string) []byte {
	base := strings.ReplaceAll(basePromptTemplate, "<absolute-task-dir>", absTaskDir)
	base = strings.ReplaceAll(base, "<reponame>", reponame)

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
