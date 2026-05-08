# AGENTS

## Allowed Paths
- /home/billyb/workspaces/billy-tui/

## Prohibited Actions
- Modify files outside /home/billyb/workspaces/billy-tui/
- Delete files without explicit instruction
- Create files not listed in an active task
- Connect to any network endpoint other than the configured billy-runtime address
- Modify billy-runtime source files

## Reporting Requirement
Every agent session must end with a list of all files created or modified.

## Project Type
go-tool — test with `go test ./...`, build with `go build ./...`, no VPS deploy.

## Governance
This project is governed by /home/billyb/workspaces/AGENT_OS.md.
All architectural changes require an ADR entry in billy-runtime/docs/adr/.
Build steps are tracked in billy-runtime/docs/CODEX_BUILD_PLAN.md.
