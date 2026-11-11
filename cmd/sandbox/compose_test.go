package sandbox

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/urfave/cli/v2"
)

func TestResolveEgress(t *testing.T) {
	t.Run("presets union and sort", func(t *testing.T) {
		got, err := resolveEgress([]string{"npm", "github"}, nil)
		if err != nil {
			t.Fatalf("resolveEgress: %v", err)
		}
		want := []string{
			"codeload.github.com", "github.com", "objects.githubusercontent.com",
			"raw.githubusercontent.com", "registry.npmjs.org",
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})

	t.Run("explicit hosts merge and dedupe", func(t *testing.T) {
		got, err := resolveEgress([]string{"npm"}, []string{"registry.npmjs.org", "example.com", " "})
		if err != nil {
			t.Fatalf("resolveEgress: %v", err)
		}
		want := []string{"example.com", "registry.npmjs.org"}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})

	t.Run("nothing means unrestricted", func(t *testing.T) {
		got, err := resolveEgress(nil, nil)
		if err != nil {
			t.Fatalf("resolveEgress: %v", err)
		}
		if len(got) != 0 {
			t.Errorf("got %v, want empty", got)
		}
	})

	t.Run("unknown preset names the real ones", func(t *testing.T) {
		_, err := resolveEgress([]string{"go-modules"}, nil)
		if err == nil {
			t.Fatal("want an error for an unknown preset")
		}
		for _, name := range egressPresetNames() {
			if !strings.Contains(err.Error(), name) {
				t.Errorf("error does not list %q: %v", name, err)
			}
		}
	})
}

func TestParseKeyValues(t *testing.T) {
	got, err := parseKeyValues([]string{"A=1", "B=with=equals", "C="})
	if err != nil {
		t.Fatalf("parseKeyValues: %v", err)
	}
	want := map[string]string{"A": "1", "B": "with=equals", "C": ""}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
	if _, err := parseKeyValues([]string{"novalue"}); err == nil {
		t.Error("want an error for a flag with no =")
	}
	if _, err := parseKeyValues([]string{"=1"}); err == nil {
		t.Error("want an error for an empty key")
	}
}

func TestStageDirHonoursGitignore(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, ".gitignore", "node_modules/\n*.log\n")
	writeFile(t, dir, "main.go", "package main\n")
	writeFile(t, dir, "app.log", "noise\n")
	writeFile(t, dir, filepath.Join("node_modules", "dep", "index.js"), "module.exports={}\n")

	for _, args := range [][]string{
		{"init", "-q"},
		{"config", "user.email", "t@example.com"},
		{"config", "user.name", "t"},
	} {
		cmd := exec.CommandContext(t.Context(), "git", append([]string{"-C", dir}, args...)...) //#nosec G204 -- dir is t.TempDir(), args are literals in this test
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Skipf("git unavailable: %v: %s", err, out)
		}
	}

	tree, err := stageDir(context.Background(), dir, stageOptions{})
	if err != nil {
		t.Fatalf("stageDir: %v", err)
	}
	defer func() { _ = os.Remove(tree.Path) }()

	names := tarNames(t, tree.Path)
	if !names["main.go"] {
		t.Error("main.go missing — tracked files must ship")
	}
	if names["app.log"] {
		t.Error("app.log shipped — .gitignore says it must not")
	}
	for n := range names {
		if strings.HasPrefix(n, "node_modules/") {
			t.Errorf("%s shipped — .gitignore says node_modules must not", n)
		}
	}
	if names[".git/HEAD"] {
		t.Error(".git shipped without IncludeGit")
	}
}

func TestStageDirWithoutGitUsesDefaultExcludes(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "main.go", "package main\n")
	writeFile(t, dir, filepath.Join("node_modules", "dep", "index.js"), "x\n")
	writeFile(t, dir, filepath.Join("dist", "bundle.js"), "x\n")
	writeFile(t, dir, filepath.Join("src", "keep.ts"), "x\n")

	tree, err := stageDir(context.Background(), dir, stageOptions{Exclude: []string{"src"}})
	if err != nil {
		t.Fatalf("stageDir: %v", err)
	}
	defer func() { _ = os.Remove(tree.Path) }()

	names := tarNames(t, tree.Path)
	if !names["main.go"] {
		t.Error("main.go missing")
	}
	for _, unwanted := range []string{"node_modules/dep/index.js", "dist/bundle.js", "src/keep.ts"} {
		if names[unwanted] {
			t.Errorf("%s shipped — it is excluded", unwanted)
		}
	}
}

func TestStageExcluded(t *testing.T) {
	for _, tc := range []struct {
		rel  string
		want bool
	}{
		{"node_modules", true},
		{"node_modules/a/b.js", true},
		{"src/node_modules/x.js", true},
		{"src/main.go", false},
		{"nodes/main.go", false},
	} {
		if got := stageExcluded(tc.rel, stageDefaultExcludes); got != tc.want {
			t.Errorf("stageExcluded(%q) = %v, want %v", tc.rel, got, tc.want)
		}
	}
}

// TestUntarIntoRefusesEscape covers the path-traversal guard. The archive
// is built inside a sandbox, so it is untrusted input on the way back out.
func TestUntarInto(t *testing.T) {
	t.Run("refuses an entry above the root", func(t *testing.T) {
		var buf bytes.Buffer
		tw := tar.NewWriter(&buf)
		body := []byte("owned")
		if err := tw.WriteHeader(&tar.Header{
			Name: "../escaped.txt", Mode: 0o600, Size: int64(len(body)), Typeflag: tar.TypeReg,
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(body); err != nil {
			t.Fatal(err)
		}
		if err := tw.Close(); err != nil {
			t.Fatal(err)
		}

		root := t.TempDir()
		err := untarInto(&buf, root)
		// filepath.Clean("/../escaped.txt") lands back at the root, so the
		// entry must either be refused or land inside root. Never above it.
		if err == nil {
			if _, statErr := os.Stat(filepath.Join(filepath.Dir(root), "escaped.txt")); !errors.Is(statErr, os.ErrNotExist) {
				t.Fatal("archive escaped the extraction root")
			}
		}
	})

	t.Run("extracts a normal tree", func(t *testing.T) {
		var buf bytes.Buffer
		tw := tar.NewWriter(&buf)
		body := []byte("hello")
		if err := tw.WriteHeader(&tar.Header{
			Name: "coverage/report.txt", Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg,
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(body); err != nil {
			t.Fatal(err)
		}
		if err := tw.Close(); err != nil {
			t.Fatal(err)
		}

		root := t.TempDir()
		if err := untarInto(&buf, root); err != nil {
			t.Fatalf("untarInto: %v", err)
		}
		got, err := os.ReadFile(filepath.Join(root, "coverage", "report.txt"))
		if err != nil {
			t.Fatalf("read extracted file: %v", err)
		}
		if string(got) != "hello" {
			t.Errorf("content = %q, want %q", got, "hello")
		}
	})
}

func writeFile(t *testing.T, dir, rel, body string) {
	t.Helper()
	p := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func tarNames(t *testing.T, path string) map[string]bool {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	names := map[string]bool{}
	tr := tar.NewReader(f)
	for {
		hdr, err := tr.Next()
		if err != nil {
			return names
		}
		names[hdr.Name] = true
	}
}

// TestSplitDirAndCommand pins the `--` handling. urfave/cli passes the
// separator through as a plain argument, so a live run once shipped
// "-- ls -la" to bash and the shell failed on it.
func TestSplitDirAndCommand(t *testing.T) {
	for _, tc := range []struct {
		name    string
		args    []string
		wantDir string
		wantCmd string
		wantErr bool
	}{
		{"with separator", []string{".", "--", "bun", "test"}, ".", "bun test", false},
		{"without separator", []string{".", "bun", "test"}, ".", "bun test", false},
		{"separator and a shell line", []string{"./src", "--", "echo", "a;", "echo", "b"}, "./src", "echo a; echo b", false},
		{"no command", []string{"."}, "", "", true},
		{"separator but no command", []string{".", "--"}, "", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app := &cli.App{
				Commands: []*cli.Command{{
					Name: "offload",
					Action: func(c *cli.Context) error {
						dir, cmd, err := splitDirAndCommand(c)
						if tc.wantErr {
							if err == nil {
								t.Errorf("want an error, got dir=%q cmd=%q", dir, cmd)
							}
							return nil
						}
						if err != nil {
							t.Errorf("splitDirAndCommand: %v", err)
							return nil
						}
						if dir != tc.wantDir {
							t.Errorf("dir = %q, want %q", dir, tc.wantDir)
						}
						if cmd != tc.wantCmd {
							t.Errorf("cmd = %q, want %q", cmd, tc.wantCmd)
						}
						return nil
					},
				}},
			}
			if err := app.Run(append([]string{"createos", "offload"}, tc.args...)); err != nil {
				t.Fatalf("app.Run: %v", err)
			}
		})
	}
}
