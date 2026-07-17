# Workspace templates

`base/` is the built-in workspace template — the tree `gogents runtime create`
materializes when `--template` is omitted.

## Editing these files does nothing

The binary uses a copy of `base/` **embedded at build time**. The files here are
kept as plain files so you can read and copy them; editing them in place does
not change what `runtime create` writes.

To make your own template take effect, copy the tree and point `--template` at
the copy:

```bash
cp -r base ./my-tpl
$EDITOR ./my-tpl/.claude/CLAUDE.md

gogents runtime create my-worker --template ./my-tpl \
    --repo https://github.com/<owner>/<repo> \
    --agent claude --model claude-sonnet-5 --source local
```

Or keep a directory of named templates and select them by name:

```bash
export GOGENTS_TEMPLATES=~/gogents-templates   # holds rust-svc/, docs-writer/, …
cp -r base $GOGENTS_TEMPLATES/rust-svc
gogents runtime create my-worker --template rust-svc --repo … --source local
```

## Layout

This directory is the container; `base/` is the tree root. A template tree's
relative paths are the workspace's relative paths — there is no extra
`templates/` layer inside a tree. `$GOGENTS_TEMPLATES` points at the container
level, mirroring this layout, so `cp -r base $GOGENTS_TEMPLATES/<name>` works
as-is.

The resolved tree is the whole content: it is never merged with the built-in
one. See the project README's "Custom templates" section for the full
resolution rules and the `*.tmpl` variables.
