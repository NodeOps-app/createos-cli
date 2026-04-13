// Package deploy provides the deploy command for creating new deployments.
package deploy

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pterm/pterm"
	"github.com/urfave/cli/v2"

	"github.com/NodeOps-app/createos-cli/internal/api"
	"github.com/NodeOps-app/createos-cli/internal/config"
	"github.com/NodeOps-app/createos-cli/internal/git"
	"github.com/NodeOps-app/createos-cli/internal/terminal"
)

const maxZipSize = 50 * 1024 * 1024 // 50 MB

// defaultIgnorePatterns are files/dirs excluded when zipping for upload.
var defaultIgnorePatterns = []string{
	// Version control
	".git",

	// CreateOS config
	".createos.json",

	// Secrets and credentials
	".env",
	".env.*",
	"*.pem",
	"*.key",
	"*.p12",
	"*.pfx",
	"*.crt",
	"*.cer",
	"*.jks",
	".npmrc",
	".pypirc",
	"credentials.json",
	"service-account*.json",

	// Dependencies
	"node_modules",
	".venv",
	"venv",
	"vendor", // Go

	// Build artifacts
	"target", // Rust
	"coverage",
	".nyc_output",
	".pytest_cache",
	"__pycache__",

	// Database files
	"*.sqlite",
	"*.sqlite3",
	"*.db",

	// Log files
	"*.log",

	// Terraform state
	".terraform",
	"terraform.tfstate",
	"terraform.tfstate.*",

	// OS/editor noise
	".DS_Store",
	"Thumbs.db",
	".idea",
	".vscode",
	"*.swp",
	"*.swo",
}

// NewDeployCommand returns the deploy command.
func NewDeployCommand() *cli.Command {
	return &cli.Command{
		Name:  "deploy",
		Usage: "Deploy your project to CreateOS",
		Description: "Creates a new deployment for the current project.\n\n" +
			"   The deploy method is chosen automatically based on your project type:\n" +
			"     VCS (GitHub) projects  → triggers from the latest commit\n" +
			"     Upload projects        → zips and uploads the current directory\n" +
			"     Image projects         → deploys the specified Docker image\n\n" +
			"   Link your project first with 'createos init' if you haven't already.",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  "project",
				Usage: "Project ID (auto-detected from .createos.json)",
			},
			&cli.StringFlag{
				Name:  "branch",
				Usage: "Branch to deploy from (VCS projects only, defaults to repo default branch)",
			},
			&cli.StringFlag{
				Name:  "image",
				Usage: "Docker image to deploy (image projects only, e.g. nginx:latest)",
			},
			&cli.StringFlag{
				Name:  "dir",
				Value: ".",
				Usage: "Directory to deploy (upload projects only)",
			},
		},
		Action: func(c *cli.Context) error {
			client, ok := c.App.Metadata[api.ClientKey].(*api.APIClient)
			if !ok {
				return fmt.Errorf("you're not signed in — run 'createos login' to get started")
			}

			projectID := c.String("project")
			if projectID == "" {
				cfg, err := config.FindProjectConfig()
				if err != nil {
					return err
				}
				if cfg != nil {
					projectID = cfg.ProjectID
				}
			}

			// If still no project, try to auto-detect from git remote
			if projectID == "" {
				dir, _ := os.Getwd()
				repoFullName := git.GetRemoteFullName(dir)
				if repoFullName != "" {
					projects, err := client.ListProjects()
					if err == nil {
						for _, p := range projects {
							if p.Status != "active" {
								continue
							}
							if p.Type != "vcs" && p.Type != "githubImport" {
								continue
							}
							var src api.VCSSource
							if err := json.Unmarshal(p.Source, &src); err != nil {
								continue
							}
							if src.VCSFullName == repoFullName {
								pterm.Info.Printf("Detected project %s from git remote (%s)\n", p.DisplayName, repoFullName)
								projectID = p.ID
								break
							}
						}
					}
				}
			}

			if projectID == "" {
				return fmt.Errorf("no project linked to this directory\n\n  Link a project first:\n    createos init\n\n  Or specify one:\n    createos deploy --project <id>")
			}

			project, err := client.GetProject(projectID)
			if err != nil {
				return err
			}

			// Validate flag/type combinations
			isVCS := project.Type == "vcs" || project.Type == "githubImport"
			if c.IsSet("branch") && !isVCS {
				return fmt.Errorf("--branch is only supported for Git-connected projects (this project uses %q deployment)", project.Type)
			}
			if c.IsSet("dir") && project.Type != "upload" {
				return fmt.Errorf("--dir is only supported for upload projects (this project uses %q deployment)", project.Type)
			}

			// Route based on project type
			switch {
			case c.IsSet("image") || project.Type == "image":
				return deployImage(c, client, project)
			case project.Type == "upload":
				return deployUpload(c, client, project)
			case project.Type == "vcs" || project.Type == "githubImport":
				return deployVCS(c, client, project)
			default:
				return fmt.Errorf("unsupported project type %q — please deploy from the dashboard", project.Type)
			}
		},
	}
}

// deployVCS triggers a new deployment from the latest commit, optionally on a specific branch.
func deployVCS(c *cli.Context, client *api.APIClient, project *api.Project) error {
	branch := c.String("branch")

	if branch == "" && terminal.IsInteractive() {
		result, err := pterm.DefaultInteractiveTextInput.
			WithDefaultText("Branch to deploy (leave empty for default branch)").
			Show()
		if err != nil {
			return err
		}
		branch = strings.TrimSpace(result)
	}

	branchLabel := "default branch"
	if branch != "" {
		branchLabel = branch
	}

	pterm.Info.Printf("Deploying %s from %s...\n", project.DisplayName, branchLabel)

	deployment, err := client.TriggerLatestDeployment(project.ID, branch)
	if err != nil {
		return err
	}

	return waitForDeployment(client, project.ID, deployment)
}

// deployUpload zips the local directory and uploads it.
func deployUpload(c *cli.Context, client *api.APIClient, project *api.Project) error {
	dir := c.String("dir")
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return err
	}

	info, err := os.Stat(absDir)
	if err != nil || !info.IsDir() {
		return fmt.Errorf("directory %q not found", dir)
	}

	pterm.Info.Printf("Deploying %s from %s...\n", project.DisplayName, absDir)

	// Create temporary zip
	zipFile, err := os.CreateTemp("", "createos-deploy-*.zip")
	if err != nil {
		return fmt.Errorf("could not create temp file: %w", err)
	}
	defer os.Remove(zipFile.Name()) //nolint:errcheck
	defer zipFile.Close()           //nolint:errcheck

	spinner, _ := pterm.DefaultSpinner.Start("Packaging files...")

	if err := createZip(zipFile, absDir); err != nil {
		spinner.Fail("Packaging failed")
		return err
	}

	stat, _ := zipFile.Stat()
	if stat != nil && stat.Size() > maxZipSize {
		spinner.Fail("Package too large")
		return fmt.Errorf("deployment package is %d MB (max %d MB)\n\n  Tip: check that node_modules, .git, and build artifacts are excluded",
			stat.Size()/(1024*1024), maxZipSize/(1024*1024))
	}

	spinner.UpdateText("Uploading...")

	// Close before uploading so the file is flushed
	if err := zipFile.Close(); err != nil { //nolint:govet
		return fmt.Errorf("could not flush deployment package: %w", err)
	}

	deployment, err := client.UploadDeploymentZip(project.ID, zipFile.Name())
	if err != nil {
		spinner.Fail("Upload failed")
		return err
	}

	spinner.Success("Uploaded")

	return waitForDeployment(client, project.ID, deployment)
}

// deployImage deploys a Docker image.
func deployImage(c *cli.Context, client *api.APIClient, project *api.Project) error {
	image := c.String("image")
	if image == "" {
		if !terminal.IsInteractive() {
			return fmt.Errorf("please provide a Docker image with --image\n\n  Example:\n    createos deploy --image nginx:latest")
		}
		result, err := pterm.DefaultInteractiveTextInput.
			WithDefaultText("Docker image (e.g. nginx:latest)").
			Show()
		if err != nil || result == "" {
			return fmt.Errorf("no image provided")
		}
		image = result
	}

	pterm.Info.Printf("Deploying %s with image %s...\n", project.DisplayName, image)

	deployment, err := client.CreateDeployment(project.ID, map[string]any{
		"image": image,
	})
	if err != nil {
		return err
	}

	return waitForDeployment(client, project.ID, deployment)
}

// waitForDeployment streams build logs while building, then runtime logs on success.
func waitForDeployment(client *api.APIClient, projectID string, deployment *api.Deployment) error {
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
				continue // transient error, keep polling
			}

			// Stream new build log lines
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
				fmt.Println()
				if d.Extra.Endpoint != "" {
					url := d.Extra.Endpoint
					if !strings.HasPrefix(url, "http") {
						url = "https://" + url
					}
					pterm.Info.Printf("Live at: %s\n", url)
					fmt.Println()
				}
				// Stream initial runtime logs
				streamRuntimeLogs(client, projectID, deployment.ID)
				return nil
			case "failed", "error", "cancelled":
				fmt.Println()
				pterm.Error.Println("Deployment failed")
				return fmt.Errorf("deployment %s failed with status: %s", d.ID, d.Status)
			}
		}
	}
}

// streamRuntimeLogs fetches and prints runtime logs after a successful deployment.
func streamRuntimeLogs(client *api.APIClient, projectID, deploymentID string) {
	logs, err := client.GetDeploymentLogs(projectID, deploymentID)
	if err != nil || logs == "" {
		pterm.Println(pterm.Gray("  View logs:    createos deployments logs"))
		pterm.Println(pterm.Gray("  Redeploy:     createos deploy"))
		return
	}
	fmt.Println("  Runtime logs:")
	fmt.Println()
	for _, line := range strings.Split(strings.TrimRight(logs, "\n"), "\n") {
		fmt.Println("  " + line)
	}
	fmt.Println()
	pterm.Println(pterm.Gray("  Follow logs:  createos deployments logs --follow"))
	pterm.Println(pterm.Gray("  Redeploy:     createos deploy"))
}

// loadGitignorePatterns reads .gitignore from srcDir and returns usable patterns.
func loadGitignorePatterns(srcDir string) []string {
	data, err := os.ReadFile(filepath.Join(srcDir, ".gitignore")) // #nosec G304 -- srcDir is from filepath.Abs, filename is a constant
	if err != nil {
		return nil
	}
	var patterns []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		// Skip comments and empty lines
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// Skip negations — we don't support re-including files
		if strings.HasPrefix(line, "!") {
			continue
		}
		// Strip trailing slash (directory marker) — we handle dirs via SkipDir
		line = strings.TrimSuffix(line, "/")
		// Strip leading slash (root-anchored) — use basename matching
		line = strings.TrimPrefix(line, "/")
		if line != "" {
			patterns = append(patterns, line)
		}
	}
	return patterns
}

// createZip creates a zip archive of the directory, excluding default ignore patterns and .gitignore rules.
func createZip(w io.Writer, srcDir string) error {
	zw := zip.NewWriter(w)
	defer zw.Close() //nolint:errcheck

	ignorePatterns := append(defaultIgnorePatterns, loadGitignorePatterns(srcDir)...) //nolint:gocritic

	return filepath.Walk(srcDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}

		// Skip root
		if relPath == "." {
			return nil
		}

		// Check ignore patterns against basename and full relative path
		baseName := filepath.Base(relPath)
		for _, pattern := range ignorePatterns {
			matchedBase, _ := filepath.Match(pattern, baseName)
			matchedRel, _ := filepath.Match(pattern, filepath.ToSlash(relPath))
			if matchedBase || matchedRel {
				if info.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
		}

		// Skip symlinks
		if info.Mode()&os.ModeSymlink != 0 {
			return nil
		}

		if info.IsDir() {
			return nil
		}

		// Skip files larger than 10MB individually
		if info.Size() > 10*1024*1024 {
			return nil
		}

		header, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		header.Name = filepath.ToSlash(relPath)
		header.Method = zip.Deflate

		writer, err := zw.CreateHeader(header)
		if err != nil {
			return err
		}

		f, err := os.Open(path) // #nosec G304,G122 -- path comes from filepath.Walk on a local directory
		if err != nil {
			return err
		}
		defer f.Close() //nolint:errcheck

		_, err = io.Copy(writer, f)
		return err
	})
}
