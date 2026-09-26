package sandbox

import (
	"archive/tar"
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/NodeOps-app/createos-cli/internal/api"
)

// stageDefaultExcludes are directories a build regenerates. They are big,
// they are usually the largest thing in a tree, and shipping them is the
// difference between a 2 MB upload and a 900 MB one. This list only
// applies outside a git repository — inside one, .gitignore already says
// what belongs, and it says it better than any fixed list can.
var stageDefaultExcludes = []string{
	".git", "node_modules", "target", "__pycache__", ".venv", "venv",
	"dist", "build", ".next", ".turbo", ".cache", "vendor",
}

// stageMaxBytes caps the upload. The file API refuses more than 500 MB,
// and hitting that limit after a two-minute upload is a bad way to find
// out. Failing early with the measured size names the problem instead.
const stageMaxBytes int64 = 500 << 20

// stageOptions tunes what stageDir packs.
type stageOptions struct {
	// IncludeGit ships the .git directory. Orca needs it (its remote git
	// reads the history); offload and matrix do not, and it is often the
	// bulk of the payload.
	IncludeGit bool
	// Exclude adds path prefixes to skip, on top of .gitignore.
	Exclude []string
}

// stagedTree is a packed directory ready to upload.
type stagedTree struct {
	Path  string // temp tar on the local disk; the caller removes it
	Size  int64
	Files int
}

// stageDir packs dir into a tar file on local disk and returns its path.
// UploadFile needs the length up front, so the archive is staged rather
// than streamed.
//
// Inside a git repository the file list comes from `git ls-files --cached
// --others --exclude-standard`: tracked files plus untracked ones that
// .gitignore does not exclude. That is "what the user sees on their
// laptop" minus the build output, and it needs no exclude list of ours.
// Outside a repository there is no such signal, so stageDefaultExcludes
// stands in.
func stageDir(ctx context.Context, dir string, opts stageOptions) (*stagedTree, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("resolve %s: %w", dir, err)
	}
	info, err := os.Stat(abs) // #nosec G703 -- abs comes from filepath.Abs of a user-named directory
	if err != nil {
		return nil, fmt.Errorf("no such directory: %s", dir)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%s is a file, not a directory", dir)
	}

	paths, err := stageFileList(ctx, abs, opts)
	if err != nil {
		return nil, err
	}
	if len(paths) == 0 {
		return nil, fmt.Errorf("%s has nothing to send — every file in it is ignored or excluded", dir)
	}

	tmp, err := os.CreateTemp("", "createos-stage-*.tar")
	if err != nil {
		return nil, err
	}
	defer func() { _ = tmp.Close() }() //nolint:errcheck // the Stat below owns the real error

	tw := tar.NewWriter(tmp)
	for _, rel := range paths {
		if err = stageTarAppend(tw, abs, rel); err != nil {
			_ = tw.Close()            //nolint:errcheck // already unwinding
			_ = os.Remove(tmp.Name()) //nolint:errcheck // best-effort cleanup
			return nil, err
		}
	}
	if err = tw.Close(); err != nil {
		_ = os.Remove(tmp.Name()) //nolint:errcheck // best-effort cleanup
		return nil, err
	}
	st, err := tmp.Stat()
	if err != nil {
		_ = os.Remove(tmp.Name()) //nolint:errcheck // best-effort cleanup
		return nil, err
	}
	if st.Size() > stageMaxBytes {
		_ = os.Remove(tmp.Name()) //nolint:errcheck // best-effort cleanup
		return nil, fmt.Errorf(
			"%s packs to %s, over the %s upload limit\n\n  Exclude what the sandbox does not need:\n    --exclude <dir>  (repeatable)",
			dir, humanBytes(st.Size()), humanBytes(stageMaxBytes))
	}
	return &stagedTree{Path: tmp.Name(), Size: st.Size(), Files: len(paths)}, nil
}

// stageFileList returns the repo-relative paths to pack.
func stageFileList(ctx context.Context, abs string, opts stageOptions) ([]string, error) {
	paths, gitErr := stageGitFileList(ctx, abs)
	if gitErr != nil {
		var walkErr error
		if paths, walkErr = stageWalkFileList(abs); walkErr != nil {
			return nil, walkErr
		}
	} else if opts.IncludeGit {
		// git ls-files never lists .git itself, so walk it separately.
		gitDir, err := stageWalkDir(abs, ".git")
		if err != nil {
			return nil, err
		}
		paths = append(paths, gitDir...)
	}
	if len(opts.Exclude) == 0 {
		return paths, nil
	}
	kept := paths[:0]
	for _, p := range paths {
		if !stageExcluded(p, opts.Exclude) {
			kept = append(kept, p)
		}
	}
	return kept, nil
}

// stageGitFileList asks git what the working tree holds. A non-nil error
// means "not a git repository" (or no git binary), not a hard failure.
func stageGitFileList(ctx context.Context, abs string) ([]string, error) {
	out, err := exec.CommandContext(ctx, "git", "-C", abs, //#nosec G204,G702 -- abs is filepath.Abs of a user-named directory
		"ls-files", "-z", "--cached", "--others", "--exclude-standard").Output()
	if err != nil {
		return nil, err
	}
	paths := make([]string, 0, 4096)
	for _, p := range strings.Split(string(out), "\x00") {
		if p != "" {
			paths = append(paths, p)
		}
	}
	return paths, nil
}

// stageWalkFileList is the non-git fallback: walk the tree, skipping the
// directories a build regenerates.
func stageWalkFileList(abs string) ([]string, error) {
	var paths []string
	err := filepath.WalkDir(abs, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil //nolint:nilerr // an unreadable entry is not worth failing the whole stage
		}
		rel, relErr := filepath.Rel(abs, p)
		if relErr != nil || rel == "." {
			return nil //nolint:nilerr
		}
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if stageExcluded(rel, stageDefaultExcludes) {
				return filepath.SkipDir
			}
			return nil
		}
		paths = append(paths, rel)
		return nil
	})
	return paths, err
}

// stageWalkDir collects every file under abs/sub, relative to abs.
func stageWalkDir(abs, sub string) ([]string, error) {
	var paths []string
	err := filepath.WalkDir(filepath.Join(abs, sub), func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil || d.IsDir() {
			return nil //nolint:nilerr
		}
		if rel, relErr := filepath.Rel(abs, p); relErr == nil {
			paths = append(paths, filepath.ToSlash(rel))
		}
		return nil
	})
	return paths, err
}

// stageExcluded reports whether rel is excluded.
//
// An exclude matches two ways: as a path prefix ("build/out" excludes
// "build/out/app.js"), or as any single segment of the path
// ("node_modules" excludes "src/node_modules/x.js"). The segment rule is
// the one that matters — a monorepo has a node_modules under every
// package, and a user who writes --exclude node_modules means all of them.
func stageExcluded(rel string, excludes []string) bool {
	segments := strings.Split(rel, "/")
	for _, ex := range excludes {
		ex = strings.Trim(filepath.ToSlash(ex), "/")
		if ex == "" {
			continue
		}
		if rel == ex || strings.HasPrefix(rel, ex+"/") {
			return true
		}
		if !strings.Contains(ex, "/") {
			for _, seg := range segments {
				if seg == ex {
					return true
				}
			}
		}
	}
	return false
}

func stageTarAppend(tw *tar.Writer, root, rel string) error {
	abs := filepath.Join(root, rel)
	info, err := os.Lstat(abs) // #nosec G703 -- rel comes from git ls-files or a walk of root, never ".."
	if err != nil {
		return nil //nolint:nilerr // a file deleted mid-walk is not fatal
	}
	link := ""
	if info.Mode()&os.ModeSymlink != 0 {
		if link, err = os.Readlink(abs); err != nil {
			return nil //nolint:nilerr
		}
	} else if !info.Mode().IsRegular() {
		return nil // sockets, fifos, devices have no place in a checkout
	}
	hdr, err := tar.FileInfoHeader(info, link)
	if err != nil {
		return err
	}
	hdr.Name = filepath.ToSlash(rel)
	if err = tw.WriteHeader(hdr); err != nil {
		return err
	}
	if link != "" || !info.Mode().IsRegular() {
		return nil
	}
	f, err := os.Open(abs) // #nosec G304,G703 -- see the Lstat note above
	if err != nil {
		return nil //nolint:nilerr
	}
	defer func() { _ = f.Close() }() //nolint:errcheck // read-only handle
	_, err = io.Copy(tw, f)
	return err
}

// shipTree uploads a staged tar and unpacks it at remoteDir inside the
// sandbox. The tar is removed from the sandbox afterwards so it does not
// double the payload's footprint on the box's disk — and, for matrix, so
// it is not copied into every fork.
func shipTree(ctx context.Context, client *api.SandboxClient, id string, tree *stagedTree, remoteDir string) error {
	f, err := os.Open(tree.Path) // #nosec G304,G703 -- path is from os.CreateTemp
	if err != nil {
		return fmt.Errorf("open staged tar: %w", err)
	}
	defer func() { _ = f.Close() }() //nolint:errcheck // read-only handle

	const remoteTar = "/tmp/createos-stage.tar"
	start := time.Now()
	if upErr := client.UploadFile(ctx, id, remoteTar, f, tree.Size); upErr != nil {
		return fmt.Errorf("upload %s to %s after %s: %w",
			humanBytes(tree.Size), id, time.Since(start).Round(time.Millisecond), upErr)
	}
	unpack := fmt.Sprintf("set -eu\nmkdir -p %s\ntar -xf %s -C %s\nrm -f %s\n",
		shellQuote(remoteDir), remoteTar, shellQuote(remoteDir), remoteTar)
	resp, err := client.ExecSandbox(ctx, id, api.SandboxExecReq{Cmd: "bash", Args: []string{"-lc", unpack}})
	if err != nil {
		return fmt.Errorf("unpack in %s: %w", id, err)
	}
	if resp.Result.ExitCode != 0 {
		return fmt.Errorf("unpack in %s exited %d: %s", id, resp.Result.ExitCode, strings.TrimSpace(resp.Result.Stderr))
	}
	return nil
}
