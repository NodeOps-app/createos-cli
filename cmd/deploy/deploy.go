// Package deploy provides the deploy command for creating new deployments.
package deploy

import (
	"archive/zip"
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
	"github.com/NodeOps-app/createos-cli/internal/terminal"
)

const maxZipSize = 50 * 1024 * 1024 // 50 MB

// defaultIgnorePatterns are files/dirs excluded when zipping for upload.
var defaultIgnorePatterns = []string{
	".git",
	".gitignore",
	".createos.json",
	"node_modules",
	".env",
	".env.*",
	"__pycache__",
	".venv",
	"venv",
	".DS_Store",
	"Thumbs.db",
	".idea",
	".vscode",
	"*.swp",
	"*.swo",
	"target",       // Rust
	"vendor",       // Go (optional, but common to exclude)
	"dist",         // built output — may need to include for some projects
	"coverage",
	".nyc_output",
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
				if cfg == nil {
					return fmt.Errorf("no project linked to this directory\n\n  Link a project first:\n    createos init\n\n  Or specify one:\n    createos deploy --project <id>")
				}
				projectID = cfg.ProjectID
			}

			project, err := client.GetProject(projectID)
			if err != nil {
				return err
			}

			// Route based on project type
			switch {
			case c.IsSet("image") || project.Type == "image":
				return deployImage(c, client, project)
			case project.Type == "upload":
				return deployUpload(c, client, project)
			default:
				// VCS (GitHub) projects and anything else
				return deployVCS(c, client, project)
			}
		},
	}
}

// deployVCS triggers a deployment from the latest commit on a branch.
func deployVCS(c *cli.Context, client *api.APIClient, project *api.Project) error {
	branch := c.String("branch")

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
	zipFile.Close() //nolint:errcheck

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

// waitForDeployment polls until the deployment succeeds, fails, or times out.
func waitForDeployment(client *api.APIClient, projectID string, deployment *api.Deployment) error {
	spinner, _ := pterm.DefaultSpinner.Start(fmt.Sprintf("Deploying (v%d)...", deployment.VersionNumber))

	timeout := time.After(5 * time.Minute)
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-timeout:
			spinner.Warning("Deployment is still in progress — check back with: createos deployments logs")
			return nil
		case <-ticker.C:
			d, err := client.GetDeployment(projectID, deployment.ID)
			if err != nil {
				continue // transient error, keep polling
			}

			switch d.Status {
			case "successful", "running", "active", "deployed":
				spinner.Success(fmt.Sprintf("Deployed (v%d)", d.VersionNumber))
				fmt.Println()
				if d.Extra.Endpoint != "" {
					url := d.Extra.Endpoint
					if !strings.HasPrefix(url, "http") {
						url = "https://" + url
					}
					pterm.Info.Printf("Live at: %s\n", url)
				}
				fmt.Println()
				pterm.Println(pterm.Gray("  View logs:   createos deployments logs"))
				pterm.Println(pterm.Gray("  Redeploy:    createos deploy"))
				return nil
			case "failed", "error", "cancelled":
				spinner.Fail(fmt.Sprintf("Deployment failed (v%d)", d.VersionNumber))
				fmt.Println()
				pterm.Println(pterm.Gray("  View build logs:  createos deployments build-logs"))
				return fmt.Errorf("deployment %s failed with status: %s", d.ID, d.Status)
			default:
				spinner.UpdateText(fmt.Sprintf("Deploying (v%d) — %s...", d.VersionNumber, d.Status))
			}
		}
	}
}

// createZip creates a zip archive of the directory, excluding default ignore patterns.
func createZip(w io.Writer, srcDir string) error {
	zw := zip.NewWriter(w)
	defer zw.Close() //nolint:errcheck

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

		// Check ignore patterns
		baseName := filepath.Base(relPath)
		for _, pattern := range defaultIgnorePatterns {
			if matched, _ := filepath.Match(pattern, baseName); matched {
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

		f, err := os.Open(path) //nolint:gosec
		if err != nil {
			return err
		}
		defer f.Close() //nolint:errcheck

		_, err = io.Copy(writer, f)
		return err
	})
}
