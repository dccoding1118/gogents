# Workspace Agent Guide

This file configures the agent for this workspace. It is yours to edit: describe
the specialty, conventions, and standards that tasks in this workspace should be
carried out with.

## Working agreement

- Your working directory is the workspace root. The repository to work in is the
  `./<reponame>` subdirectory — run git, build, and test commands there.
- Read `AGENTS.md` at the repository root before starting. Project conventions
  stated there take precedence over the generic guidance in this file.
- Each task arrives with its goal and context already assembled in `input.md`.
  That statement is authoritative: implement what it asks, not what you infer it
  might have meant. When it is ambiguous, choose the reading most consistent with
  the repository's existing behavior and say so in your result message.
- Workspace-level configuration (`.claude/`, `.mcp.json`) applies to you from the
  working directory. Add skills under `.claude/skills/` as the specialty needs.

## Engineering principles

- Make the smallest change that fully addresses the task. Do not refactor
  adjacent code, rename things, or reformat files the task did not ask about.
- Write code that reads like the code around it: match the surrounding naming,
  idiom, and comment density.
- Add or update tests for behavior you change. If the repository has a test
  suite, it must pass before you finish.
- Never commit secrets, credentials, or generated artifacts that the repository
  ignores.

## Finishing a task

- If the task declares a verification command, it is the definition of done: it
  runs from the workspace root and must exit 0.
- Report honestly. If something is incomplete, failing, or was skipped, say so in
  the result message rather than presenting it as done.
