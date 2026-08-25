# elgit, Git for Humans

Git for humans, safely. A from-scratch Go rewrite of [legit](https://github.com/frostming/legit): the same everyday workflows (switch, sync, publish, unpublish, undo, branches) with faster startup and stricter safety rules. elgit shells out to your own git binary, so your config, credentials, and hooks always apply.

## Why

legit (the Python tool) died of its dependencies: two abandoned libraries, string-parsed stash matching, shell-string alias installs. elgit keeps the workflows and replaces the plumbing:

- Static binary, ~2ms startup instead of a ~300ms interpreter plus GitPython import.
- Stashes are tracked by commit OID in a ledger under the common git dir (`.git/elgit/ledger.json`), never by grepping stash messages or slicing list lines. A restore applies the stash by OID and drops it only after re-verifying the OID, so concurrent changes to the stash stack cannot redirect it onto the wrong entry.
- A merge or rebase conflict stops the flow and leaves the repository in its conflicted state. elgit never runs `--abort` for you.
- A failed fetch or rejected push restores your stashed work before exiting: you end where you started.
- Every failure that happened after a stash tells you exactly where your changes live and how to get them back, including stashes parked on other branches by `elgit sync <branch>`.
- elgit spawns no shells itself: every git call is an argv array, and branch names are passed behind `--end-of-options` or as fully qualified refs so a branch named like an option can never be executed as one. `GIT_TERMINAL_PROMPT=0` makes missing credentials fail fast instead of hanging.
- A per-repository lock in the common git dir (shared by all worktrees) prevents two elgit runs from corrupting each other's stash flow. Lock creation is atomic; only one racer can claim a stale lock.
- Sync skips the merge entirely when already up to date and skips the push when there is nothing to push.
- `--fake` is a real dry-run: read-only queries execute so output reflects actual state; mutations are suppressed and echoed, and nothing at all is written, not even the lock or the ledger.

## Quick Install

```sh
# One-line installer (macOS & Linux)
curl -fsSL https://raw.githubusercontent.com/EstebanForge/elgit/main/scripts/install.sh | bash

# Or with Homebrew
brew install EstebanForge/tap/elgit

# Or from source (Go 1.24+)
go install github.com/EstebanForge/elgit@latest
```

The installer picks the right build for your platform, installs to `/usr/local/bin` (or `~/.local/bin` without write access), and skips the download when the requested version is already installed. Set `VERSION=x.y.z` or `INSTALL_DIR=/path` to override.

Requires git 2.24 or newer on PATH (for `--end-of-options`); elgit checks at startup and refuses to run otherwise.

## Usage

```sh
elgit sw                  # pick a branch interactively (filter + arrows); remote-only branches included
elgit sw <branch>         # switch directly, stashing and restoring local changes
elgit sync                 # stash, fetch, merge-or-rebase, push, restore
elgit sync <branch>        # sync another branch, then return to the current one
elgit pub                  # publish the current branch to the remote (push -u)
elgit unpub <branch>       # delete the branch on the remote
elgit undo                 # undo the last commit (--hard also discards its changes)
elgit branches [pattern]   # list branches with publication state
elgit config               # show effective settings and where they come from
elgit --install            # install the commands above as git aliases
elgit --uninstall          # remove elgit and legacy legit aliases
```

With aliases installed, `git sync`, `git sw feature`, and bare `git sw` (the interactive picker) work directly. elgit never installs a `switch` alias, so the native `git switch` command is never shadowed. The alias value quotes the elgit path, so installs from paths containing spaces work; git itself executes `!` aliases through a shell, which is why the quoting is there.

`elgit sw` without an argument opens a filterable branch list (charmbracelet/huh, the same select stack as wicket-cli-tools): arrows to move, type to filter, enter to select, ESC or Ctrl-C to cancel. Remote-only branches are listed with a `(remote only)` marker and a fresh `fetch --prune`; picking one creates the local branch with tracking. Without a terminal the same prompt degrades to a numbered list, so scripts keep working.

## Settings

elgit reads the standard git config hierarchy (repo, global, includes), `[legit]` section. The keys match the Python tool, so an existing setup carries over:

| Key | Default | Meaning |
|---|---|---|
| `legit.remote` | first remote | which remote to work against |
| `legit.remoteFallback` | false | when `legit.remote` does not exist, fall back to the first remote instead of erroring |
| `legit.smartMerge` | true | sync heuristic: rebase for linear history, merge when the local side has merge commits; false defers to `pull.rebase` and `pull.ff` |

## Safety rules

1. Stashes are recorded by commit OID with branch and purpose. Restores apply by OID and drop only after a post-drop verification; a missing OID means the stash was already handled and the record is cleared.
2. Merge and rebase conflicts stop the run. The conflicted state stays; the error prints the exact `git rebase --continue` / `--abort` commands and where the stash is. The stash is deliberately not restored onto a conflicted tree.
3. elgit never force-pushes and never loops on a push race; it restores the stash (if any), reports, and exits.
4. Mutating commands refuse to run while a merge, rebase, cherry-pick, revert, bisect, or am is in progress, and before the first commit exists.
5. The stash ledger survives crashes. The next mutating run warns about unrestored entries with their OIDs.
6. Undo warns when HEAD is a merge commit before removing it.
7. A failed fetch or push restores the stashed work automatically: clean tree, original error, no lost context.

## Differences from legit

- `--config` no longer opens an editor on a private ini file; settings live in git config and `elgit config` shows them read-only.
- The interactive prompt when `legit.remote` is invalid is gone; fix the config or set `legit.remoteFallback`.
- Sync performs up-to-date and nothing-to-push checks to avoid needless merges and network pushes.
- Colors respect `NO_COLOR` and terminal detection instead of a settings toggle.

## Development

```sh
make check         # fmt + vet + golangci-lint + race tests + build (CI parity)
make build-signed  # build, ad-hoc sign on macOS, install to ~/.local/bin
make cross-compile # 4-platform builds into dist/
make release       # full release artifacts + checksums into dist/
```

The test suite runs real git repositories in temporary directories, including conflict, rebase, worktree, and concurrency states.

## Releasing

1. Update `CHANGELOG.md` (notes inside `RELEASE:START x.y.z` / `RELEASE:END x.y.z` markers), bump `VERSION` and `internal/cli/root.go` `Version` together (`make check-version` enforces the match).
2. Commit, then tag and push the tag: `git tag 0.1.0 && git push origin 0.1.0`. Tags carry no `v` prefix.
3. The release workflow runs lint + tests, builds all platforms (universal macOS via lipo), packages versioned and Homebrew-facing tarballs with checksums, publishes the GitHub Release using the CHANGELOG notes, commits the `VERSION` marker, and dispatches the `homebrew-tap` formula update.

The tap update job needs a `TAP_GITHUB_TOKEN` secret (classic PAT with `repo` scope on `EstebanForge/homebrew-tap`) on this repository.

## Credit

Workflows and UX copied from Kenneth Reitz's and frostming's legit.
