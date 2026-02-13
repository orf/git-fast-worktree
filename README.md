# git-fast-worktree

A macOS tool that creates [git worktrees](https://git-scm.com/docs/git-worktree) using APFS copy-on-write cloning instead of `git checkout`. For large monorepos this can be orders of magnitude faster than `git worktree add`.

## Install

```bash
go install github.com/orf/git-fast-worktree@latest
```

## Usage

Run from within a git repository:

```bash
# Create a worktree at the given path
git fast-worktree add /tmp/my-worktree

# Create a worktree on a new branch
git fast-worktree add -b my-branch /tmp/my-worktree

# Create a worktree at a specific commit
git fast-worktree add /tmp/my-worktree origin/main
```

The CLI mirrors `git worktree add` flags:

```
$ git-fast-worktree add --help
Create a worktree using APFS clonefile

Usage:
  git-fast-worktree add [flags] <path> [<commit-ish>]

Flags:
  -b, --branch string         create a new branch
  -B, --force-branch string   create or reset a branch
  -h, --help                  help for add
      --no-track              do not set up tracking mode
```

## How it works

1. `git worktree add --no-checkout` registers the worktree with git
2. Each top-level entry in the source repo (excluding `.git`) is cloned into the worktree using the APFS [`clonefile`](https://www.manpagez.com/man/2/clonefile/) syscall, which recursively clones entire directory trees without copying data
3. `git reset --no-refresh` populates the git index to match HEAD

Because `clonefile` is copy-on-write, the worktree initially shares all data blocks with the source repo and only allocates new storage when files are modified.

## Limitations

- **macOS only** - relies on the APFS `clonefile` syscall
- **Same volume only** - source and destination must be on the same APFS volume
- Copies the working tree as-is, including untracked and ignored files from the source
