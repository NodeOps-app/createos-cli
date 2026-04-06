package projects

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/pterm/pterm"
	"github.com/urfave/cli/v2"

	"github.com/NodeOps-app/createos-cli/internal/api"
	"github.com/NodeOps-app/createos-cli/internal/config"
	"github.com/NodeOps-app/createos-cli/internal/terminal"
)

// validProjectTypes lists the accepted values for the --type flag.
var validProjectTypes = []string{"vcs", "image", "upload"}

// settingsFlagMap maps editable field names to their CLI flag names.
var settingsFlagMap = map[string]string{
	"port":           "port",
	"directoryPath":  "directory-path",
	"buildDir":       "build-dir",
	"buildFlag":      "build-flag",
	"buildCommand":   "build-command",
	"runCommand":     "run-command",
	"installCommand": "install-command",
	"runFlag":        "run-flag",
}

// promptOrder controls the order in which editable fields are prompted.
var promptOrder = []string{
	"port", "directoryPath", "installCommand", "buildCommand",
	"buildDir", "buildFlag", "runCommand", "runFlag",
}

// promptLabels maps editable field names to human-readable prompt labels.
var promptLabels = map[string]string{
	"port":           "Port",
	"directoryPath":  "Directory path",
	"buildDir":       "Build output directory",
	"buildFlag":      "Build flags",
	"buildCommand":   "Build command",
	"runCommand":     "Run command",
	"installCommand": "Install command",
	"runFlag":        "Run flags",
}

// frameworkSelection holds the result of the interactive framework/runtime picker.
type frameworkSelection struct {
	framework string // framework name (e.g. "nextjs") — empty for standalone runtimes
	runtime   string // runtime version from runtimes array (e.g. "node:20", "golang:1.25")
	editables map[string]api.EditableField
}

func newAddCommand() *cli.Command {
	return &cli.Command{
		Name:  "add",
		Usage: "Create a new project",
		Description: `Create a new project on CreateOS. In interactive mode (TTY), you'll be
guided through each step. In non-interactive mode (CI/scripts), provide
all required flags:

  Image project:
    createos projects add --name "My API" --unique-name my-api \
      --type image --port 8080

  Upload project:
    createos projects add --name "My App" --unique-name my-app \
      --type upload --framework nextjs --runtime node:20

  VCS project:
    createos projects add --name "My App" --unique-name my-app \
      --type vcs --framework nextjs --runtime node:20 \
      --github-owner myorg --repo myorg/my-app`,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "name", Usage: "Display name for the project"},
			&cli.StringFlag{Name: "unique-name", Usage: "Unique name (lowercase, 4-32 chars)"},
			&cli.StringFlag{Name: "type", Usage: "Project type (vcs, image, upload)"},
			&cli.StringFlag{Name: "description", Usage: "Project description"},
			&cli.StringFlag{Name: "framework", Usage: "Framework (e.g. nextjs, reactjs-spa, vite-spa)"},
			&cli.StringFlag{Name: "runtime", Usage: "Runtime (e.g. node:20, golang:1.25, dockerfile)"},
			&cli.IntFlag{Name: "port", Usage: "Port the application listens on"},
			&cli.StringFlag{Name: "install-command", Usage: "Install command (e.g. npm install)"},
			&cli.StringFlag{Name: "build-command", Usage: "Build command (e.g. npm run build)"},
			&cli.StringFlag{Name: "run-command", Usage: "Run command (e.g. npm run start)"},
			&cli.StringFlag{Name: "build-dir", Usage: "Build output directory"},
			&cli.StringFlag{Name: "build-flag", Usage: "Build flags"},
			&cli.StringFlag{Name: "run-flag", Usage: "Run flags"},
			&cli.StringFlag{Name: "directory-path", Usage: "Root directory path (default: .)"},
			// VCS source flags (non-interactive mode)
			&cli.StringFlag{Name: "github-owner", Usage: "GitHub account/org name (for VCS projects)"},
			&cli.StringFlag{Name: "repo", Usage: "GitHub repository full name, e.g. owner/repo (for VCS projects)"},
		},
		Action: func(c *cli.Context) error {
			client, ok := c.App.Metadata[api.ClientKey].(*api.APIClient)
			if !ok {
				return fmt.Errorf("you're not signed in — run 'createos login' to get started")
			}

			displayName := c.String("name")
			uniqueName := c.String("unique-name")
			projectType := c.String("type")

			// VCS-specific fields collected during interactive flow
			var selectedInstallationID string
			var selectedRepoID string

			// Framework/runtime selection
			var sel frameworkSelection
			sel.framework = c.String("framework")
			sel.runtime = c.String("runtime")

			// Settings collected from prompts or flags
			settings := map[string]any{}

			if terminal.IsInteractive() {
				if displayName == "" {
					result, err := pterm.DefaultInteractiveTextInput.
						WithDefaultText("Display name").
						Show()
					if err != nil {
						return fmt.Errorf("could not read input: %w", err)
					}
					displayName = result
				}

				if uniqueName == "" {
					result, err := pterm.DefaultInteractiveTextInput.
						WithDefaultText("Unique name (lowercase, 4-32 chars)").
						Show()
					if err != nil {
						return fmt.Errorf("could not read input: %w", err)
					}
					uniqueName = result
				}

				if projectType == "" {
					result, err := pterm.DefaultInteractiveSelect.
						WithDefaultText("Project type").
						WithOptions(validProjectTypes).
						Show()
					if err != nil {
						return fmt.Errorf("could not read input: %w", err)
					}
					projectType = result
				}

				// For VCS projects, prompt for GitHub account and repo
				if projectType == "vcs" {
					installationID, repoID, err := promptGitHubSource(client)
					if err != nil {
						return err
					}
					selectedInstallationID = installationID
					selectedRepoID = repoID
				}

				// Prompt for framework/runtime (VCS and upload)
				if projectType == "vcs" || projectType == "upload" {
					if sel.framework == "" && sel.runtime == "" {
						picked, err := promptFrameworkRuntime(client, projectType)
						if err != nil {
							return err
						}
						sel = picked
					} else {
						// Flag provided — fetch editables
						editables, err := fetchEditablesForSelection(client, sel.framework, sel.runtime)
						if err == nil {
							sel.editables = editables
						}
					}
				}

				// For image projects, prompt for port
				if projectType == "image" {
					if c.Int("port") == 0 {
						portStr, err := pterm.DefaultInteractiveTextInput.
							WithDefaultText("Port [default: 80]").
							Show()
						if err != nil {
							return fmt.Errorf("could not read input: %w", err)
						}
						if portStr == "" {
							settings["port"] = 80
						} else {
							p, err := strconv.Atoi(portStr)
							if err != nil || p < 1 || p > 65535 {
								return fmt.Errorf("invalid port %q — must be a number between 1 and 65535", portStr)
							}
							settings["port"] = p
						}
					}
				}

				// Prompt for editable settings based on the selected framework/runtime
				if sel.editables != nil {
					prompted, err := promptEditableSettings(c, sel.editables)
					if err != nil {
						return err
					}
					for k, v := range prompted {
						settings[k] = v
					}
				}
			}

			// --- Validation (covers both interactive and non-interactive) ---

			if displayName == "" {
				return fmt.Errorf("please provide a display name with --name\n\n  Example:\n    createos projects add --name \"My App\"")
			}
			if uniqueName == "" {
				return fmt.Errorf("please provide a unique name with --unique-name\n\n  Example:\n    createos projects add --unique-name my-app")
			}
			if projectType == "" {
				return fmt.Errorf("please provide a project type with --type\n\n  Valid types: vcs, image, upload\n\n  Example:\n    createos projects add --type upload")
			}
			if !isValidProjectType(projectType) {
				return fmt.Errorf("invalid project type %q\n\n  Valid types: vcs, image, upload", projectType)
			}

			// VCS source: resolve from --github-owner and --repo in non-interactive mode
			if projectType == "vcs" && selectedInstallationID == "" {
				githubOwner := c.String("github-owner")
				repoFullName := c.String("repo")
				if githubOwner == "" || repoFullName == "" {
					return fmt.Errorf("vcs projects require a GitHub source\n\n  In interactive mode these are prompted automatically.\n  In non-interactive mode, provide:\n    --github-owner <username-or-org> --repo <owner/repo-name>\n\n  Example:\n    createos projects add --type vcs --github-owner myorg --repo myorg/my-app")
				}
				instID, rID, err := resolveGitHubSource(client, githubOwner, repoFullName)
				if err != nil {
					return err
				}
				selectedInstallationID = instID
				selectedRepoID = rID
			}

			// Framework/runtime validation for upload and VCS
			if (projectType == "vcs" || projectType == "upload") && sel.framework == "" && sel.runtime == "" {
				return fmt.Errorf("please provide --framework and/or --runtime\n\n  Examples:\n    --framework nextjs\n    --runtime dockerfile\n    --framework nextjs --runtime node:20")
			}

			// Image projects require a port
			if projectType == "image" {
				if c.IsSet("port") {
					settings["port"] = c.Int("port")
				}
				if _, hasPort := settings["port"]; !hasPort {
					return fmt.Errorf("image projects require a port\n\n  Example:\n    createos projects add --type image --port 8080")
				}
			}

			req := api.CreateProjectRequest{
				DisplayName: displayName,
				UniqueName:  uniqueName,
				Type:        projectType,
			}

			if desc := c.String("description"); desc != "" {
				req.Description = &desc
			}

			// Build source for VCS projects
			if projectType == "vcs" {
				source := map[string]string{
					"vcsName":           "github",
					"vcsInstallationId": selectedInstallationID,
					"vcsRepoId":         selectedRepoID,
				}
				sourceJSON, err := json.Marshal(source)
				if err != nil {
					return fmt.Errorf("could not encode source: %w", err)
				}
				req.Source = sourceJSON
			}

			// Set framework/runtime in settings
			if sel.framework != "" {
				settings["framework"] = sel.framework
			}
			if sel.runtime != "" {
				settings["runtime"] = sel.runtime
			}

			// Apply flag overrides (flags take precedence over interactive prompts)
			applyFlagOverrides(c, settings)

			settingsJSON, err := json.Marshal(settings)
			if err != nil {
				return fmt.Errorf("could not encode settings: %w", err)
			}
			req.Settings = settingsJSON

			id, err := client.CreateProject(req)
			if err != nil {
				return err
			}

			pterm.Success.Printf("Project created successfully!\n")
			pterm.Println(pterm.Gray("  ID: " + id))

			// Prompt to link the current directory to the new project
			if terminal.IsInteractive() {
				fmt.Println()
				link, err := pterm.DefaultInteractiveConfirm.
					WithDefaultText("Link this directory to the new project?").
					WithDefaultValue(true).
					Show()
				if err == nil && link {
					dir, err := os.Getwd()
					if err != nil {
						return fmt.Errorf("could not determine current directory: %w", err)
					}

					cfg := config.ProjectConfig{
						ProjectID:   id,
						ProjectName: displayName,
					}

					envs, err := client.ListEnvironments(id)
					if err == nil && len(envs) == 1 {
						cfg.EnvironmentID = envs[0].ID
					}

					if err := config.SaveProjectConfig(dir, cfg); err != nil {
						return fmt.Errorf("could not save project config: %w", err)
					}
					_ = config.EnsureGitignore(dir)

					pterm.Success.Printf("Linked to %s\n", displayName)
				} else {
					pterm.Println(pterm.Gray("  To link this directory, run:"))
					pterm.Println(pterm.Gray("    createos init --project " + id))
				}
			} else {
				pterm.Println(pterm.Gray("  To link this directory, run:"))
				pterm.Println(pterm.Gray("    createos init --project " + id))
			}

			// Prompt to trigger first deployment for VCS and image projects
			if projectType == "vcs" && terminal.IsInteractive() {
				fmt.Println()
				deploy, err := pterm.DefaultInteractiveConfirm.
					WithDefaultText("Trigger first deployment now?").
					WithDefaultValue(true).
					Show()
				if err == nil && deploy {
					pterm.Info.Printf("Deploying %s from default branch...\n", displayName)
					deployment, err := client.TriggerLatestDeployment(id, "")
					if err != nil {
						pterm.Warning.Printf("Could not trigger deployment: %s\n", err)
						pterm.Println(pterm.Gray("  You can deploy later with:"))
						pterm.Println(pterm.Gray("    createos deploy --project " + id))
						return nil
					}
					return waitForFirstDeployment(client, id, deployment)
				}
			}

			if projectType == "image" && terminal.IsInteractive() {
				fmt.Println()
				deploy, err := pterm.DefaultInteractiveConfirm.
					WithDefaultText("Deploy a Docker image now?").
					WithDefaultValue(true).
					Show()
				if err == nil && deploy {
					image, err := pterm.DefaultInteractiveTextInput.
						WithDefaultText("Docker image (e.g. nginx:latest)").
						Show()
					if err != nil || image == "" {
						pterm.Warning.Println("No image provided, skipping deployment")
						return nil
					}
					pterm.Info.Printf("Deploying %s with image %s...\n", displayName, image)
					deployment, err := client.CreateDeployment(id, map[string]any{
						"image": image,
					})
					if err != nil {
						pterm.Warning.Printf("Could not trigger deployment: %s\n", err)
						pterm.Println(pterm.Gray("  You can deploy later with:"))
						pterm.Println(pterm.Gray("    createos deploy --project " + id))
						return nil
					}
					return waitForFirstDeployment(client, id, deployment)
				}
			}

			if (projectType == "vcs" || projectType == "image") && !terminal.IsInteractive() {
				pterm.Println(pterm.Gray("  To trigger a deployment, run:"))
				pterm.Println(pterm.Gray("    createos deploy --project " + id))
			}

			return nil
		},
	}
}

// waitForFirstDeployment polls a deployment until it completes or times out.
func waitForFirstDeployment(client *api.APIClient, projectID string, deployment *api.Deployment) error {
	fmt.Println()

	timeout := time.After(10 * time.Minute)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	lastBuildLine := 0

	for {
		select {
		case <-timeout:
			fmt.Println()
			pterm.Warning.Println("Deployment is still in progress — check back with: createos deployments build-logs")
			return nil
		case <-ticker.C:
			d, err := client.GetDeployment(projectID, deployment.ID)
			if err != nil {
				continue
			}

			buildLogs, err := client.GetDeploymentBuildLogs(projectID, deployment.ID)
			if err == nil {
				for _, e := range buildLogs {
					if e.LineNumber > lastBuildLine {
						fmt.Println(e.Log)
						lastBuildLine = e.LineNumber
					}
				}
			}

			switch d.Status {
			case "successful", "running", "active", "deployed":
				fmt.Println()
				pterm.Success.Println("Deployed successfully")
				if d.Extra.Endpoint != "" {
					url := d.Extra.Endpoint
					if !strings.HasPrefix(url, "http") {
						url = "https://" + url
					}
					fmt.Println()
					pterm.Info.Printf("Live at: %s\n", url)
				}
				return nil
			case "failed", "error", "cancelled":
				fmt.Println()
				pterm.Error.Println("Deployment failed")
				return fmt.Errorf("deployment %s failed with status: %s", d.ID, d.Status)
			}
		}
	}
}

// promptFrameworkRuntime walks the user through selecting a framework/runtime.
//
// For upload projects the backend requires BOTH framework and runtime fields
// (unless using dockerfile or build-ai). The flow is:
//   - User picks a framework  → framework name is set, runtime is auto-derived
//     from the framework's runtimes array
//   - User picks a standalone runtime (dockerfile, build-ai) → only runtime is set
//   - User picks a regular runtime (nodejs, golang, …) for VCS → only runtime is set
//   - User picks a regular runtime for upload → we also prompt for a framework
func promptFrameworkRuntime(client *api.APIClient, projectType string) (frameworkSelection, error) {
	supported, err := client.ListSupportedProjectTypes()
	if err != nil {
		return frameworkSelection{}, fmt.Errorf("could not list supported frameworks: %w", err)
	}

	options := make([]string, len(supported))
	for i, s := range supported {
		if s.Type == "framework" {
			options[i] = s.Name + " (framework)"
		} else {
			options[i] = s.Name + " (runtime)"
		}
	}

	selected, err := pterm.DefaultInteractiveSelect.
		WithDefaultText("Framework / Runtime").
		WithOptions(options).
		WithFilter(true).
		Show()
	if err != nil {
		return frameworkSelection{}, fmt.Errorf("could not read input: %w", err)
	}

	// Find the selected entry
	var entry api.SupportedProjectType
	for _, s := range supported {
		label := s.Name
		if s.Type == "framework" {
			label += " (framework)"
		} else {
			label += " (runtime)"
		}
		if label == selected {
			entry = s
			break
		}
	}

	sel := frameworkSelection{editables: entry.Editables}

	if entry.Type == "framework" {
		// Framework selected — set framework name and derive runtime from runtimes array
		sel.framework = entry.Name
		if len(entry.Runtimes) > 0 {
			sel.runtime = entry.Runtimes[0]
		}
		return sel, nil
	}

	// Runtime selected
	runtimeValue := ""
	if len(entry.Runtimes) > 0 {
		runtimeValue = entry.Runtimes[0]
	}
	sel.runtime = runtimeValue

	// For upload projects, standalone runtimes (dockerfile, build-ai) are fine alone.
	// Regular runtimes need a framework too.
	standaloneRuntimes := map[string]bool{"dockerfile": true, "build-ai": true}
	if projectType == "upload" && !standaloneRuntimes[entry.Name] {
		// Need to also pick a framework that's compatible with this runtime
		var frameworks []api.SupportedProjectType
		for _, s := range supported {
			if s.Type != "framework" {
				continue
			}
			for _, rt := range s.Runtimes {
				if rt == runtimeValue {
					frameworks = append(frameworks, s)
					break
				}
			}
		}

		if len(frameworks) == 0 {
			return frameworkSelection{}, fmt.Errorf("no compatible frameworks found for runtime %q\n\n  For upload projects, try selecting a framework instead", entry.Name)
		}

		fwOptions := make([]string, len(frameworks))
		for i, fw := range frameworks {
			fwOptions[i] = fw.Name
		}

		fwSelected, err := pterm.DefaultInteractiveSelect.
			WithDefaultText("Framework (required for upload projects)").
			WithOptions(fwOptions).
			WithFilter(true).
			Show()
		if err != nil {
			return frameworkSelection{}, fmt.Errorf("could not read input: %w", err)
		}

		sel.framework = fwSelected
		// Use the framework's editables since they're more specific
		for _, fw := range frameworks {
			if fw.Name == fwSelected {
				sel.editables = fw.Editables
				break
			}
		}
	}

	return sel, nil
}

// promptEditableSettings prompts the user for each editable setting of the
// selected framework/runtime. It skips object-type fields (buildVars, runEnvs)
// and any field already set via a CLI flag.
func promptEditableSettings(c *cli.Context, editables map[string]api.EditableField) (map[string]any, error) {
	result := map[string]any{}

	for _, field := range promptOrder {
		editable, ok := editables[field]
		if !ok {
			continue
		}

		// Skip if already set via CLI flag
		flagName, hasFlagMapping := settingsFlagMap[field]
		if hasFlagMapping && c.IsSet(flagName) {
			continue
		}

		label, hasLabel := promptLabels[field]
		if !hasLabel {
			continue
		}

		defaultVal := ""
		if editable.Default != nil {
			defaultVal = fmt.Sprintf("%v", editable.Default)
		}

		promptLabel := label
		if defaultVal != "" {
			promptLabel = fmt.Sprintf("%s [default: %s]", label, defaultVal)
		}

		switch editable.Type {
		case "number":
			input, err := pterm.DefaultInteractiveTextInput.
				WithDefaultText(promptLabel).
				Show()
			if err != nil {
				return nil, fmt.Errorf("could not read input: %w", err)
			}
			if input == "" && defaultVal != "" {
				input = defaultVal
			}
			if input != "" {
				n, err := strconv.Atoi(input)
				if err != nil {
					return nil, fmt.Errorf("invalid value for %s: %q — must be a number", label, input)
				}
				result[field] = n
			}
		case "string":
			input, err := pterm.DefaultInteractiveTextInput.
				WithDefaultText(promptLabel).
				Show()
			if err != nil {
				return nil, fmt.Errorf("could not read input: %w", err)
			}
			if input == "" && defaultVal != "" {
				input = defaultVal
			}
			if input != "" {
				result[field] = input
			}
		default:
			// Skip "object" type fields (buildVars, runEnvs) — set via env commands later
		}
	}

	return result, nil
}

// applyFlagOverrides writes CLI flag values into the settings map.
// Flags always take precedence over interactive prompts.
func applyFlagOverrides(c *cli.Context, settings map[string]any) {
	if c.IsSet("port") {
		settings["port"] = c.Int("port")
	}
	for settingsKey, flagName := range settingsFlagMap {
		if settingsKey == "port" {
			continue // handled above as int
		}
		if c.IsSet(flagName) {
			settings[settingsKey] = c.String(flagName)
		}
	}
}

// promptGitHubSource walks the user through selecting a GitHub account and repository.
func promptGitHubSource(client *api.APIClient) (installationID, repoID string, err error) {
	installations, err := client.ListGithubInstallations()
	if err != nil {
		return "", "", fmt.Errorf("could not list GitHub accounts: %w", err)
	}
	if len(installations) == 0 {
		return "", "", fmt.Errorf("no GitHub accounts connected\n\n  Connect a GitHub account at https://app.createos.io first")
	}

	accountOptions := make([]string, len(installations))
	for i, inst := range installations {
		accountOptions[i] = inst.Username
	}

	selectedAccount, err := pterm.DefaultInteractiveSelect.
		WithDefaultText("GitHub account").
		WithOptions(accountOptions).
		Show()
	if err != nil {
		return "", "", fmt.Errorf("could not read input: %w", err)
	}

	var selectedInstallation api.GithubInstallation
	for _, inst := range installations {
		if inst.Username == selectedAccount {
			selectedInstallation = inst
			break
		}
	}

	installationIDStr := strconv.FormatInt(selectedInstallation.InstallationID, 10)

	repos, err := client.ListGithubRepos(installationIDStr)
	if err != nil {
		return "", "", fmt.Errorf("could not list repositories: %w", err)
	}
	if len(repos) == 0 {
		return "", "", fmt.Errorf("no repositories found for %q\n\n  Make sure the GitHub App has access to at least one repository", selectedAccount)
	}

	repoOptions := make([]string, len(repos))
	for i, repo := range repos {
		repoOptions[i] = repo.FullName
	}

	selectedRepo, err := pterm.DefaultInteractiveSelect.
		WithDefaultText("Repository").
		WithOptions(repoOptions).
		WithFilter(true).
		Show()
	if err != nil {
		return "", "", fmt.Errorf("could not read input: %w", err)
	}

	var repoIDInt int64
	for _, repo := range repos {
		if repo.FullName == selectedRepo {
			repoIDInt = repo.ID
			break
		}
	}

	return installationIDStr, strconv.FormatInt(repoIDInt, 10), nil
}

// resolveGitHubSource resolves a GitHub owner name and repo full name to
// installation ID and repo ID by querying the API.
func resolveGitHubSource(client *api.APIClient, githubOwner, repoFullName string) (installationID, repoID string, err error) {
	installations, err := client.ListGithubInstallations()
	if err != nil {
		return "", "", fmt.Errorf("could not list GitHub accounts: %w", err)
	}

	var matched api.GithubInstallation
	var found bool
	for _, inst := range installations {
		if inst.Username == githubOwner {
			matched = inst
			found = true
			break
		}
	}
	if !found {
		available := make([]string, len(installations))
		for i, inst := range installations {
			available[i] = inst.Username
		}
		return "", "", fmt.Errorf("GitHub account %q not found\n\n  Available accounts: %v", githubOwner, available)
	}

	instIDStr := strconv.FormatInt(matched.InstallationID, 10)

	repos, err := client.ListGithubRepos(instIDStr)
	if err != nil {
		return "", "", fmt.Errorf("could not list repositories for %q: %w", githubOwner, err)
	}

	for _, repo := range repos {
		if repo.FullName == repoFullName {
			return instIDStr, strconv.FormatInt(repo.ID, 10), nil
		}
	}

	return "", "", fmt.Errorf("repository %q not found under %q\n\n  Make sure the GitHub App has access to this repository", repoFullName, githubOwner)
}

// fetchEditablesForSelection fetches the editables for a framework or runtime.
func fetchEditablesForSelection(client *api.APIClient, framework, runtime string) (map[string]api.EditableField, error) {
	supported, err := client.ListSupportedProjectTypes()
	if err != nil {
		return nil, err
	}
	// Prefer framework editables if both set
	if framework != "" {
		for _, s := range supported {
			if s.Name == framework && s.Type == "framework" {
				return s.Editables, nil
			}
		}
	}
	if runtime != "" {
		for _, s := range supported {
			for _, rt := range s.Runtimes {
				if rt == runtime {
					return s.Editables, nil
				}
			}
		}
	}
	return nil, fmt.Errorf("no matching supported project type found")
}

func isValidProjectType(t string) bool {
	for _, valid := range validProjectTypes {
		if t == valid {
			return true
		}
	}
	return false
}
