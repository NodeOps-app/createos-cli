package telemetry

import (
	"context"
	"os"
	"runtime"

	"github.com/NodeOps-app/createos-cli/internal/pkg/version"
	"github.com/NodeOps-app/createos-cli/internal/terminal"
	"github.com/posthog/posthog-go"
)

// telSchemaVersion is bumped when the property shape changes in an
// incompatible way for downstream dashboards.
const telSchemaVersion = 1

// GlobalProperties returns the always-on properties attached to every event.
// The shape matches spec §5.1.
func GlobalProperties(_ context.Context) posthog.Properties {
	props := posthog.NewProperties().
		Set("tel_schema_version", telSchemaVersion).
		Set("version", version.Version).
		Set("channel", version.Channel).
		Set("commit_sha", shortCommit(version.Commit)).
		Set("goos", runtime.GOOS).
		Set("goarch", runtime.GOARCH).
		Set("os_release", osRelease()).
		Set("go_version", runtime.Version()).
		Set("is_interactive", terminal.IsInteractive())

	isCI, provider := detectCI()
	props = props.Set("is_ci", isCI).Set("ci_provider", provider)
	return props
}

// shortCommit truncates a git SHA to the first 7 chars (bounds-checked).
func shortCommit(c string) string {
	if len(c) >= 7 {
		return c[:7]
	}
	return c
}

// detectCI returns whether the process is running in CI and a best-effort
// provider name. Order matters — most specific markers checked first so that,
// e.g., a GitHub Actions runner is reported as "github" rather than the
// generic "ci".
func detectCI() (bool, string) {
	switch {
	case os.Getenv("GITHUB_ACTIONS") == "true":
		return true, "github"
	case os.Getenv("GITLAB_CI") == "true":
		return true, "gitlab"
	case os.Getenv("CIRCLECI") == "true":
		return true, "circle"
	case os.Getenv("BUILDKITE") == "true":
		return true, "buildkite"
	case os.Getenv("JENKINS_URL") != "":
		return true, "jenkins"
	case os.Getenv("TF_BUILD") == "True":
		return true, "azure"
	case os.Getenv("CI") != "":
		return true, "generic"
	}
	return false, ""
}
