---
name: create-pr
description: Create a pull request for the current branch against main, after running make ci locally. Use when asked to create/open a PR, ship the current branch, or put changes up for review in this repo.
---

# Create PR

Open a pull request for the current branch in card-backend. Always targets `main` (the repo's default branch) and always verifies `make ci` locally first — don't push a red PR.

## Workflow

1. **Check the tree.** `git status` — make sure there's nothing uncommitted that should be included or stashed. If there are uncommitted changes relevant to this work, ask whether to commit them first; don't assume.

2. **Run `make ci` locally** (build, test, lint). If it fails, fix the root cause and re-run — don't open the PR on a red build, and don't use `--no-verify` or skip lint/test to get around a failure.

3. **Gather branch context**, in parallel:
   ```bash
   git branch --show-current
   git status
   git diff main...HEAD
   git log main..HEAD --oneline
   ```
   If the current branch is `main` itself, stop and ask the user what branch name to use — don't open a PR from main.

4. **Push the branch** if it doesn't already track a remote, or if it's ahead of the remote:
   ```bash
   git push -u origin <branch>
   ```

5. **Draft title and body** from the actual commits/diff (not just the latest commit — every commit that will be in the PR):
   - Title: short (under 70 chars), imperative mood (matches this repo's log style, e.g. "Add nonce-based JWT auth endpoint").
   - Body: `## Summary` (1-3 bullets on the why, not a line-by-line diff recap) and `## Test plan` (a checklist — mention `make ci` passed, plus anything needing manual/live-DB verification, e.g. hitting an endpoint against Postgres if `make db-up` wasn't available this session).

6. **Create the PR**, explicitly targeting `main`:
   ```bash
   gh pr create --base main --title "<title>" --body "$(cat <<'EOF'
   ## Summary
   - ...

   ## Test plan
   - [ ] make ci
   - [ ] ...
   EOF
   )"
   ```

7. Return the PR URL to the user.

## Rules

- Never force-push, never skip hooks, never amend published commits, to get a PR open.
- Don't create the PR if `make ci` is failing — fix it first, or tell the user exactly what's broken and stop.
- Don't invent a test plan item that wasn't actually verified — if something couldn't be tested (e.g. no local Postgres), say so explicitly rather than checking the box.
