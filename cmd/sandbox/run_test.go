package sandbox

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/urfave/cli/v2"
)

func TestParseRunDiskFlag(t *testing.T) {
	t.Parallel()

	got, err := parseRunDiskFlag("pg-data,/data:/var/lib/postgresql/data")
	if err != nil {
		t.Fatalf("parseRunDiskFlag: %v", err)
	}
	if got.diskID != "pg-data" || got.sandboxPath != "/data" || got.containerPath != "/var/lib/postgresql/data" {
		t.Fatalf("parsed disk = %#v", got)
	}
}

func TestParseRunDiskFlagRejectsOldCreateSyntax(t *testing.T) {
	t.Parallel()

	if _, err := parseRunDiskFlag("pg-data:/var/lib/postgresql/data"); err == nil {
		t.Fatal("expected old create syntax to be rejected")
	}
}

func TestBuildDockerRunArgs(t *testing.T) {
	t.Parallel()

	got := buildDockerRunArgs(runOptions{
		image:  "postgres",
		remote: 5432,
		envs:   []string{"POSTGRES_PASSWORD=secret"},
		disks: []runDiskMount{{
			diskID:        "pg-data",
			sandboxPath:   "/data",
			containerPath: "/var/lib/postgresql/data",
		}},
		syncs: []runSyncMount{{
			localPath:     "/tmp/app",
			sandboxPath:   "/workspace",
			containerPath: "/app",
		}},
	})
	want := []string{
		"run",
		"--rm",
		"-p", "127.0.0.1:5432:5432",
		"-e", "POSTGRES_PASSWORD=secret",
		"-v", "/data:/var/lib/postgresql/data",
		"-v", "/workspace:/app",
		"postgres",
	}
	if len(got) != len(want) {
		t.Fatalf("len(args) = %d, want %d\n got=%v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("args[%d] = %q, want %q\n got=%v", i, got[i], want[i], got)
		}
	}
}

func TestParseRunSyncFlag(t *testing.T) {
	t.Parallel()

	got, err := parseRunSyncFlag("./app,/workspace:/app")
	if err != nil {
		t.Fatalf("parseRunSyncFlag: %v", err)
	}
	if got.localPath != "./app" || got.sandboxPath != "/workspace" || got.containerPath != "/app" {
		t.Fatalf("parsed sync = %#v", got)
	}
}

func TestParseRunArgsFlagsAfterImage(t *testing.T) {
	got := runParseRunArgs(t,
		"postgres",
		"--disk", "pg-data,/data:/var/lib/postgresql/data",
		"--sync", "./app,/workspace:/app",
		"--network", "db",
		"--local", "5432",
		"--remote", "5432",
		"--env", "POSTGRES_PASSWORD=secret",
		"--", "-c", "shared_buffers=256MB",
	)
	if got.image != "postgres" {
		t.Fatalf("image = %q, want postgres", got.image)
	}
	if got.local != 5432 || got.remote != 5432 {
		t.Fatalf("ports = %d/%d, want 5432/5432", got.local, got.remote)
	}
	if !reflect.DeepEqual(got.networks, []string{"db"}) {
		t.Fatalf("networks = %v, want [db]", got.networks)
	}
	if !reflect.DeepEqual(got.envs, []string{"POSTGRES_PASSWORD=secret"}) {
		t.Fatalf("envs = %v", got.envs)
	}
	if !reflect.DeepEqual(got.imageArgs, []string{"-c", "shared_buffers=256MB"}) {
		t.Fatalf("imageArgs = %v", got.imageArgs)
	}
	if len(got.disks) != 1 || got.disks[0].sandboxPath != "/data" || got.disks[0].containerPath != "/var/lib/postgresql/data" {
		t.Fatalf("disks = %#v", got.disks)
	}
	if len(got.syncs) != 1 || got.syncs[0].localPath != "./app" || got.syncs[0].sandboxPath != "/workspace" || got.syncs[0].containerPath != "/app" {
		t.Fatalf("syncs = %#v", got.syncs)
	}
}

func TestParseRunArgsDiskBeforeImage(t *testing.T) {
	got := runParseRunArgs(t, "--disk", "cache,/cache:/cache", "redis")
	if got.image != "redis" {
		t.Fatalf("image = %q, want redis", got.image)
	}
	if len(got.disks) != 1 || got.disks[0].diskID != "cache" {
		t.Fatalf("disks = %#v", got.disks)
	}
}

func TestParseRunArgsPushLocal(t *testing.T) {
	got := runParseRunArgs(t, "my-app:dev", "--push-local")
	if !got.pushLocal {
		t.Fatal("pushLocal = false, want true")
	}
}

func TestParseRunArgsRemoveSandbox(t *testing.T) {
	got := runParseRunArgs(t, "nginx", "--rm")
	if !got.removeSandbox {
		t.Fatal("removeSandbox = false, want true")
	}
}

func TestParseRunArgsRejectsPullAndPushLocal(t *testing.T) {
	if err := runParseRunArgsErr("nginx", "--pull", "--push-local"); err == nil {
		t.Fatal("expected --pull with --push-local to fail")
	}
}

func TestParseRunArgsRejectsRemoveSandboxNoFollow(t *testing.T) {
	if err := runParseRunArgsErr("nginx", "--rm", "--no-follow"); err == nil {
		t.Fatal("expected --rm with --no-follow to fail")
	}
}

func runParseRunArgsErr(argv ...string) error {
	app := &cli.App{
		Commands: []*cli.Command{{
			Name:  "run",
			Flags: newRunCommand().Flags,
			Action: func(c *cli.Context) error {
				_, err := parseRunArgs(c)
				return err
			},
		}},
	}
	return app.Run(append([]string{"app", "run"}, argv...))
}

func TestLocalImageArchiveName(t *testing.T) {
	got := localImageArchiveName("localhost:5000/my-app:dev")
	if got != "localhost-5000-my-app-dev.tar" {
		t.Fatalf("archive name = %q", got)
	}
}

func TestRunMutagenCreateArgsSetsReadableModes(t *testing.T) {
	got := runMutagenCreateArgs("sess1", "two-way-safe", "/local", "root@127.0.0.1:2222:/workspace", []string{"node_modules"})
	wantContains := map[string]bool{
		"--default-file-mode-beta=0644":      false,
		"--default-directory-mode-beta=0755": false,
		"--ignore=node_modules":              false,
	}
	for _, arg := range got {
		if _, ok := wantContains[arg]; ok {
			wantContains[arg] = true
		}
	}
	for arg, seen := range wantContains {
		if !seen {
			t.Fatalf("runMutagenCreateArgs missing %q in %v", arg, got)
		}
	}
	if got[len(got)-2] != "/local" || got[len(got)-1] != "root@127.0.0.1:2222:/workspace" {
		t.Fatalf("source/target must be last two args, got %v", got)
	}
}

func TestResolveRunSyncIdentityGeneratesDedicatedKey(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	priv, pubBytes, cleanup, err := resolveRunSyncIdentity("sb-01m10vpsz09ajsyezny04mnvfq", "")
	if err != nil {
		t.Fatalf("resolveRunSyncIdentity: %v", err)
	}
	if !strings.HasPrefix(priv, filepath.Join(home, ".config", "createos", "keys")) {
		t.Fatalf("private key path = %q, want under ~/.config/createos/keys", priv)
	}
	if len(pubBytes) == 0 {
		t.Fatal("expected public key bytes")
	}
	if _, err := os.Stat(priv); err != nil {
		t.Fatalf("private key not written: %v", err)
	}
	if _, err := os.Stat(priv + ".pub"); err != nil {
		t.Fatalf("public key not written: %v", err)
	}

	cleanup()
	if _, err := os.Stat(priv); !os.IsNotExist(err) {
		t.Fatalf("private key still exists after cleanup: %v", err)
	}
	if _, err := os.Stat(priv + ".pub"); !os.IsNotExist(err) {
		t.Fatalf("public key still exists after cleanup: %v", err)
	}
}

func runParseRunArgs(t *testing.T, argv ...string) runOptions {
	t.Helper()
	var got runOptions
	app := &cli.App{
		Commands: []*cli.Command{{
			Name:  "run",
			Flags: newRunCommand().Flags,
			Action: func(c *cli.Context) error {
				var err error
				got, err = parseRunArgs(c)
				return err
			},
		}},
	}
	if err := app.Run(append([]string{"app", "run"}, argv...)); err != nil {
		t.Fatalf("run %v: %v", argv, err)
	}
	return got
}
