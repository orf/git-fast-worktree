//go:build darwin

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/sys/unix"
)

var rootCmd = &cobra.Command{
	Use:   "git-fast-worktree",
	Short: "Create git worktrees using APFS copy-on-write cloning",
	Long:  "Creates git worktrees using APFS copy-on-write cloning instead of git checkout.\nMust be run from within a git repository on macOS with an APFS volume.",
}

var (
	branchCreate string
	branchReset  string
	noTrack      bool
)

var addCmd = &cobra.Command{
	Use:   "add [flags] <path> [<commit-ish>]",
	Short: "Create a worktree using APFS clonefile",
	Args:  cobra.RangeArgs(1, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		// Resolve source: git repo root of the current directory
		src, err := gitToplevel()
		if err != nil {
			return fmt.Errorf("not a git repository (or any parent): %w", err)
		}

		dst, err := filepath.Abs(args[0])
		if err != nil {
			return fmt.Errorf("error resolving destination path: %w", err)
		}

		var commitish string
		if len(args) == 2 {
			commitish = args[1]
		}

		if _, err := os.Stat(dst); err == nil {
			return fmt.Errorf("fatal: '%s' already exists", dst)
		}

		// Validate flags
		if branchCreate != "" && branchReset != "" {
			return fmt.Errorf("fatal: -b and -B are mutually exclusive")
		}

		total := time.Now()

		// Phase 1: Create git worktree (sets up .git file in dst)
		stepStart := time.Now()
		worktreeArgs := []string{"-C", src, "worktree", "add", "--no-checkout"}
		if branchCreate != "" {
			worktreeArgs = append(worktreeArgs, "-b", branchCreate)
		} else if branchReset != "" {
			worktreeArgs = append(worktreeArgs, "-B", branchReset)
		} else {
			worktreeArgs = append(worktreeArgs, "--detach")
		}
		if noTrack {
			worktreeArgs = append(worktreeArgs, "--no-track")
		}
		worktreeArgs = append(worktreeArgs, dst)
		if commitish != "" {
			worktreeArgs = append(worktreeArgs, commitish)
		}

		gitCmd := exec.Command("git", worktreeArgs...)
		gitCmd.Stderr = os.Stderr
		if err := gitCmd.Run(); err != nil {
			return fmt.Errorf("git worktree add failed")
		}
		println(fmt.Sprintf("worktree add: (%v)", time.Since(stepStart).Round(time.Millisecond)))

		// Phase 2: Read top-level entries from source (skip .git)
		entries, err := os.ReadDir(src)
		if err != nil {
			return fmt.Errorf("error reading source directory: %w", err)
		}

		var toClone []string
		for _, e := range entries {
			if e.Name() == ".git" {
				continue
			}
			toClone = append(toClone, e.Name())
		}

		// Phase 3: Clonefile each top-level entry in parallel
		stepStart = time.Now()
		var cloned atomic.Int64
		var cloneErrors sync.Map

		var wg sync.WaitGroup
		for _, name := range toClone {
			wg.Add(1)
			go func() {
				defer wg.Done()
				srcPath := filepath.Join(src, name)
				dstPath := filepath.Join(dst, name)
				if err := unix.Clonefile(srcPath, dstPath, unix.CLONE_NOFOLLOW); err != nil {
					cloneErrors.Store(name, err)
				} else {
					cloned.Add(1)
				}
			}()
		}
		wg.Wait()
		println(fmt.Sprintf("clonefile:    %d entries (%v)", cloned.Load(), time.Since(stepStart).Round(time.Millisecond)))

		// Phase 4: Update git index to match HEAD
		stepStart = time.Now()
		resetCmd := exec.Command("git", "-C", dst, "reset", "--no-refresh")
		resetCmd.Stderr = os.Stderr
		if err := resetCmd.Run(); err != nil {
			return fmt.Errorf("git reset: %w", err)
		}
		println(fmt.Sprintf("git reset:    (%v)", time.Since(stepStart).Round(time.Millisecond)))

		var errCount int
		cloneErrors.Range(func(key, value any) bool {
			if errCount == 0 {
				println("")
			}
			errCount++
			println(fmt.Sprintf("  %s: %v", key, value))
			return true
		})

		println(fmt.Sprintf("\ntotal: %v", time.Since(total).Round(time.Millisecond)))
		println("worktree: " + dst)

		if errCount > 0 {
			return fmt.Errorf("%d clone errors occurred", errCount)
		}
		return nil
	},
}

func init() {
	addCmd.Flags().StringVarP(&branchCreate, "branch", "b", "", "create a new branch")
	addCmd.Flags().StringVarP(&branchReset, "force-branch", "B", "", "create or reset a branch")
	addCmd.Flags().BoolVar(&noTrack, "no-track", false, "do not set up tracking mode")
}

func main() {
	rootCmd.AddCommand(addCmd)
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

// gitToplevel returns the root directory of the current git repository.
func gitToplevel() (string, error) {
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
