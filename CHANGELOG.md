# Changelog

All notable changes to elgit are documented here. The format follows the release-notes markers consumed by the release workflow: each version's notes sit between `RELEASE:START` and `RELEASE:END` comments and become the GitHub Release body verbatim.

<!-- RELEASE:START 0.2.0 -->
## 0.2.0 - 2026-09-03

The GitHub Desktop loop: commit, merge, and status join the workflow set, with interactive pickers wherever a branch name would otherwise come from memory.

### New commands

- `elgit commit` stages every modified tracked file and creates one commit. Untracked files join only with `--all`, so scratch files never ride into history. `-m` takes the summary, `-d` the optional description; with neither, a terminal gets a two-field prompt (summary required, description optional) and a non-terminal gets an error, so scripts never stall. `--amend` rewords or folds work into the last commit, prefilled with the current message. Pre-commit hooks that edit files are reported instead of lost. Never pushes.
- `elgit merge [branch]` compares first, merges second: "Merging 7 commits from stage into main" with the incoming commit list, then a plain `git merge`, so your `merge.ff` config decides fast-forward versus merge commit. Without an argument a picker lists branches with ahead/behind counts (one `for-each-ref` call, git 2.29 or newer; older git lists without counts). Remote-only branches merge from their remote-tracking ref, fetched first. An already-merged candidate short-circuits. Never pushes: `elgit sync` owns the remote.
- `elgit status` (`st`) glances at branch, upstream ahead/behind, and staged/modified/untracked counts. Read-only, no network.
- `elgit unpublish [branch]` opens a picker of published branches on a terminal, after a fresh `fetch --prune`, with the current branch marked. It is the one picker without a non-terminal fallback: it deletes a remote ref, so scripts name the branch and nothing is deleted through a prompt.

### Interactive UX

- A spinner with a message now runs during every remote wait before a menu (`Fetching branches`), so slow fetches never look like a hang. Without a terminal it degrades to one plain line.
- Backing out of any picker (ESC, or closed stdin) exits 0 quietly instead of surfacing an abort error.
- The commit message prompt never runs when there is nothing to commit.

### Safety

- `elgit commit` and `elgit merge` are never installed as git aliases, and `git status` stays native: the three commands users know by muscle memory are never shadowed.
- `elgit merge` refuses a dirty working tree. There is no stash flow around a merge on purpose: a conflicted stash restore can never land on top of a committed merge, the one state `git merge --abort` cannot unwind.
- `elgit commit` never pushes and stages tracked modifications only; untracked files join with `--all`, never by accident.

### Compatibility

- Ahead/behind counts use the `for-each-ref` ahead-behind atom, which needs git 2.29 or newer; older git lists branches without counts, and the 2.24 startup gate is unchanged.

<!-- RELEASE:END 0.2.0 -->

<!-- RELEASE:START 0.1.0 -->
## 0.1.0 - 2026-08-25

Initial release. A from-scratch Go rewrite of [legit](https://github.com/frostming/legit) ("Git for Humans"): the same everyday workflows, with a static binary, strict safety rules, and an interactive branch picker.

### Commands

- `elgit sw` opens a filterable branch picker (arrows + type-to-filter, ESC/Ctrl-C cancel); remote-only branches are listed with a `(remote only)` marker and switching creates the local tracking branch. `elgit sw <branch>` switches directly with a unique prefix match.
- `elgit sync [branch]` stashes, fetches, integrates (rebase for linear history, merge when the local side has merge commits), pushes, restores. Skips the merge when already up to date and the push when there is nothing to send.
- `elgit publish [<branch>]` pushes with upstream tracking; `elgit unpublish <branch>` deletes the remote branch.
- `elgit undo [--hard]` removes the last commit, warning when HEAD is a merge.
- `elgit branches [pattern]` lists branches with `(published)` / `(unpublished)` / `(remote only)` state; patterns match across `/`.
- `elgit config` shows effective `[legit]` settings and the file each comes from.
- `elgit --install` / `--uninstall` manage git aliases (`git sw`, `git sync`, ...), including removal of legacy legit aliases; `git sw` opens the interactive switcher, and a `switch` alias is never installed so the native `git switch` command is never shadowed.

### Safety

- Stashes tracked by commit OID in a ledger under the common git dir; restores apply by OID and drop only after re-verification, so stash-stack races cannot restore the wrong entry.
- Merge/rebase conflicts stop the run: the conflicted state stays, the error prints the exact recovery commands. elgit never auto-aborts.
- Failed fetch or rejected push restores the stashed work before exiting.
- Per-repository lock (atomic creation, stale takeover, ownership-checked release) shared by all worktrees serializes elgit runs.
- Branch names are passed behind `--end-of-options` or as fully qualified refs: a branch named `-f` or `--mirror` can never execute as a git option.
- `--fake` dry-run: queries execute for real output, mutations are echoed only, and nothing at all is written.
- No shell anywhere in elgit itself; missing credentials fail fast instead of hanging.

### Compatibility

- Reads the legacy `[legit]` git-config keys (`legit.remote`, `legit.remoteFallback`, `legit.smartMerge`), so existing legit setups carry over.
- Requires git 2.24 or newer; checked at startup.
<!-- RELEASE:END 0.1.0 -->
