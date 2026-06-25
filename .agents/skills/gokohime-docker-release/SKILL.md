---
name: gokohime-docker-release
description: Rebuild Gokohime Docker containers, verify status, then create a gitmoji-style commit and push. Use when the user asks to rebuild/redeploy this repo with Docker and commit/push the resulting changes.
metadata:
  short-description: Rebuild Gokohime Docker and push changes
---

# Gokohime Docker Release

Use this workflow from the repository root `/home/ubuntu/Gokohime`.

## Workflow

1. Check the worktree before changing anything:
   - `git status --short`
   - Review changed files enough to avoid committing unrelated user work.
2. Run targeted validation:
   - For `.kadd` or KTV changes, run `go test ./plugin/kk`.
   - For broader Go changes, run relevant package tests or `go test ./...` when practical.
3. Rebuild and restart services:
   - `docker compose up -d --build`
   - Prefer this over manually removing containers unless troubleshooting requires it.
4. Verify deployment:
   - `docker compose ps`
   - If a service is restarting or unhealthy, inspect logs with `docker compose logs --tail=80 <service>`.
5. Commit in gitmoji style:
   - Stage only intended files with explicit paths.
   - Use a concise message, for example `✨ fix .kadd multi-word song parsing`.
   - Include this skill file when the user asks to persist the rebuild/commit/push workflow.
6. Push the current branch:
   - Confirm with `git branch --show-current`.
   - Run `git push`.
   - Do not print embedded remote credentials in the final response.

## Guardrails

- Do not commit generated caches, local data, logs, or unrelated files.
- Do not rewrite history unless explicitly requested.
- Keep final handoff short: changed files, Docker status, commit hash, and push result.
