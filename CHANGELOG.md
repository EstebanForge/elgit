# Changelog

All notable changes to elgit are documented here. The format follows the release-notes markers consumed by the release workflow: each version's notes sit between `RELEASE:START` and `RELEASE:END` comments and become the GitHub Release body verbatim.

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
