package api

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"
)

// Project represents a CreateOS project
type Project struct {
	ID          string          `json:"id"`
	UniqueName  string          `json:"uniqueName"`
	DisplayName string          `json:"displayName"`
	Description *string         `json:"description"`
	Status      string          `json:"status"`
	Type        string          `json:"type"`
	Source      json.RawMessage `json:"source,omitempty"`
	CreatedAt   time.Time       `json:"createdAt"`
	UpdatedAt   time.Time       `json:"updatedAt"`
}

// VCSSource represents the source details for a VCS project.
type VCSSource struct {
	VCSName     string `json:"vcsName"`
	VCSFullName string `json:"vcsFullName"`
}

// CreateProjectRequest is the request body for creating a new project.
type CreateProjectRequest struct {
	DisplayName string          `json:"displayName"`
	UniqueName  string          `json:"uniqueName"`
	Type        string          `json:"type"`
	Description *string         `json:"description,omitempty"`
	Source      json.RawMessage `json:"source,omitempty"`
	Settings    json.RawMessage `json:"settings"`
}

// CreateProject creates a new project and returns the new project ID.
func (c *APIClient) CreateProject(req CreateProjectRequest) (string, error) {
	var result Response[struct {
		ID string `json:"id"`
	}]
	resp, err := c.Client.R().
		SetResult(&result).
		SetBody(req).
		Post("/v1/projects")
	if err != nil {
		return "", err
	}
	if resp.IsError() {
		return "", ParseAPIError(resp.StatusCode(), resp.Body())
	}
	return result.Data.ID, nil
}

// CheckUniqueNameAvailable checks whether a project unique name is available.
func (c *APIClient) CheckUniqueNameAvailable(uniqueName string) (bool, error) {
	var result Response[struct {
		IsAvailable bool `json:"isAvailable"`
	}]
	resp, err := c.Client.R().
		SetResult(&result).
		SetBody(map[string]string{"uniqueName": uniqueName}).
		Post("/v1/projects/available-unique-name")
	if err != nil {
		return false, err
	}
	if resp.IsError() {
		return false, ParseAPIError(resp.StatusCode(), resp.Body())
	}
	return result.Data.IsAvailable, nil
}

// GithubInstallation represents a connected GitHub account.
type GithubInstallation struct {
	InstallationID int64  `json:"installationId"`
	Username       string `json:"username"`
	OwnerID        int64  `json:"ownerId"`
	AvatarURL      string `json:"avatarUrl"`
}

// ListGithubInstallations returns the user's connected GitHub accounts.
func (c *APIClient) ListGithubInstallations() ([]GithubInstallation, error) {
	var result Response[[]GithubInstallation]
	resp, err := c.Client.R().
		SetResult(&result).
		Get("/v1/app-installations/github/installations")
	if err != nil {
		return nil, err
	}
	if resp.IsError() {
		return nil, ParseAPIError(resp.StatusCode(), resp.Body())
	}
	return result.Data, nil
}

// GithubRepo represents a GitHub repository accessible via an installation.
type GithubRepo struct {
	ID            int64  `json:"id"`
	Name          string `json:"name"`
	FullName      string `json:"full_name"`
	Private       bool   `json:"private"`
	DefaultBranch string `json:"default_branch"`
}

// ListGithubRepos returns all repositories for a GitHub installation.
func (c *APIClient) ListGithubRepos(installationID string) ([]GithubRepo, error) {
	var result Response[[]GithubRepo]
	resp, err := c.Client.R().
		SetResult(&result).
		Get("/v1/app-installations/github/accounts/" + installationID + "/repositories")
	if err != nil {
		return nil, err
	}
	if resp.IsError() {
		return nil, ParseAPIError(resp.StatusCode(), resp.Body())
	}
	return result.Data, nil
}

// EditableField describes a configurable setting for a framework/runtime.
type EditableField struct {
	Type     string `json:"type"`
	Default  any    `json:"default"`
	Required bool   `json:"required,omitempty"`
}

// SupportedProjectType represents a framework or runtime supported by CreateOS.
type SupportedProjectType struct {
	Type      string                   `json:"type"`
	Name      string                   `json:"name"`
	Runtimes  []string                 `json:"runtimes"`
	Editables map[string]EditableField `json:"editables"`
}

// ListSupportedProjectTypes returns the available frameworks and runtimes.
func (c *APIClient) ListSupportedProjectTypes() ([]SupportedProjectType, error) {
	var result Response[[]SupportedProjectType]
	resp, err := c.Client.R().
		SetResult(&result).
		Get("/v1/projects/supported")
	if err != nil {
		return nil, err
	}
	if resp.IsError() {
		return nil, ParseAPIError(resp.StatusCode(), resp.Body())
	}
	return result.Data, nil
}

// ListProjects returns all projects for the authenticated user.
func (c *APIClient) ListProjects() ([]Project, error) {
	var result PaginatedResponse[Project]
	resp, err := c.Client.R().
		SetResult(&result).
		Get("/v1/projects")
	if err != nil {
		return nil, err
	}
	if resp.IsError() {
		return nil, ParseAPIError(resp.StatusCode(), resp.Body())
	}
	return result.Data.Items, nil
}

// GetProject returns a single project by ID.
func (c *APIClient) GetProject(id string) (*Project, error) {
	var result Response[Project]
	resp, err := c.Client.R().
		SetResult(&result).
		Get("/v1/projects/" + id)
	if err != nil {
		return nil, err
	}
	if resp.IsError() {
		return nil, ParseAPIError(resp.StatusCode(), resp.Body())
	}
	return &result.Data, nil
}

// DeleteProject permanently deletes a project by ID.
func (c *APIClient) DeleteProject(id string) error {
	resp, err := c.Client.R().
		Delete("/v1/projects/" + id)
	if err != nil {
		return err
	}
	if resp.IsError() {
		return ParseAPIError(resp.StatusCode(), resp.Body())
	}
	return nil
}

// SuspendProject suspends a running project.
func (c *APIClient) SuspendProject(id string) error {
	resp, err := c.Client.R().
		Put("/v1/projects/" + id + "/suspend")
	if err != nil {
		return err
	}
	if resp.IsError() {
		return ParseAPIError(resp.StatusCode(), resp.Body())
	}
	return nil
}

// UnsuspendProject resumes a suspended project.
func (c *APIClient) UnsuspendProject(id string) error {
	resp, err := c.Client.R().
		Put("/v1/projects/" + id + "/unsuspend")
	if err != nil {
		return err
	}
	if resp.IsError() {
		return ParseAPIError(resp.StatusCode(), resp.Body())
	}
	return nil
}

// GetUser returns the currently authenticated user.
func (c *APIClient) GetUser() (*User, error) {
	var result Response[User]
	resp, err := c.Client.R().
		SetResult(&result).
		Get("/v1/users/me")
	if err != nil {
		return nil, err
	}

	if resp.IsError() {
		return nil, ParseAPIError(resp.StatusCode(), resp.Body())
	}

	return &result.Data, nil
}

// Skill represents a skill available in the catalog.
type Skill struct {
	ID         string    `json:"id"`
	UserID     string    `json:"userId"`
	Name       string    `json:"name"`
	UniqueName string    `json:"uniqueName"`
	UseCases   string    `json:"useCases"`
	Overview   string    `json:"overview"`
	Status     string    `json:"status"`
	Categories []string  `json:"categories"`
	Amount     float64   `json:"amount"`
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
}

// PurchasedSkillItem represents a skill that has been purchased by the user.
type PurchasedSkillItem struct {
	ID              string    `json:"id"`
	SkillID         string    `json:"skillId"`
	UserID          string    `json:"userId"`
	PurchasedAmount float64   `json:"purchasedAmount"`
	CreatedAt       time.Time `json:"createdAt"`
	Skill           Skill     `json:"skill,omitempty"`
}

// ListMyPurchasedSkills returns all skills purchased by the authenticated user.
func (c *APIClient) ListMyPurchasedSkills() ([]PurchasedSkillItem, error) {
	var result Response[[]PurchasedSkillItem]
	resp, err := c.Client.R().
		SetResult(&result).
		Get("/v1/skills/purchased")
	if err != nil {
		return nil, err
	}

	if resp.IsError() {
		return nil, ParseAPIError(resp.StatusCode(), resp.Body())
	}

	return result.Data, nil
}

// GetDownloadURL holds the download URI response for a purchased skill.
type GetDownloadURL struct {
	DownloadURL string `json:"downloadUri"`
}

// GetSkillDownloadURL returns the download URL for a purchased skill by its purchase ID.
func (c *APIClient) GetSkillDownloadURL(purchasedID string) (string, error) {
	var result Response[GetDownloadURL]
	resp, err := c.Client.R().
		SetResult(&result).
		Get("/v1/skills/purchased/" + purchasedID + "/download")
	if err != nil {
		return "", err
	}

	if resp.IsError() {
		return "", ParseAPIError(resp.StatusCode(), resp.Body())
	}

	return result.Data.DownloadURL, nil
}

// PaginatedResponse wraps a paginated list API response envelope.
type PaginatedResponse[T any] struct {
	Data struct {
		Items      []T        `json:"data"`
		Pagination Pagination `json:"pagination"`
	} `json:"data"`
}

// Pagination holds metadata about a paginated response.
type Pagination struct {
	Total  int `json:"total"`
	Limit  int `json:"limit"`
	Offset int `json:"offset"`
	Count  int `json:"count"`
}

// ListAvailableSkillsForPurchase returns a paginated list of skills available for purchase.
func (c *APIClient) ListAvailableSkillsForPurchase(searchText string, offset int, limit int) ([]Skill, Pagination, error) {
	queryParams := map[string]string{
		"limit":  strconv.Itoa(limit),
		"offset": strconv.Itoa(offset),
	}
	if searchText != "" {
		queryParams["name"] = searchText
	}
	var result PaginatedResponse[Skill]
	resp, err := c.Client.R().
		SetResult(&result).
		SetQueryParams(queryParams).
		Get("/v1/skills/available")
	if err != nil {
		return nil, Pagination{}, err
	}
	if resp.IsError() {
		return nil, Pagination{}, ParseAPIError(resp.StatusCode(), resp.Body())
	}
	return result.Data.Items, result.Data.Pagination, nil
}

// DeploymentExtra holds extra deployment metadata.
type DeploymentExtra struct {
	Endpoint string `json:"endpoint"`
}

// DeploymentSource holds git/vcs source info for a deployment.
type DeploymentSource struct {
	Branch        string `json:"branch"`
	Commit        string `json:"commit"`
	CommitMessage string `json:"commitMessage"`
}

// Deployment represents a project deployment.
type Deployment struct {
	ID            string            `json:"id"`
	ProjectID     string            `json:"projectId"`
	Status        string            `json:"status"`
	VersionNumber int               `json:"versionNumber"`
	Source        *DeploymentSource `json:"source"`
	Extra         DeploymentExtra   `json:"extra"`
	CreatedAt     time.Time         `json:"createdAt"`
	UpdatedAt     time.Time         `json:"updatedAt"`
}

// ListDeployments returns all deployments for a project.
func (c *APIClient) ListDeployments(projectID string) ([]Deployment, error) {
	var result PaginatedResponse[Deployment]
	resp, err := c.Client.R().
		SetResult(&result).
		Get("/v1/projects/" + projectID + "/deployments")
	if err != nil {
		return nil, err
	}
	if resp.IsError() {
		return nil, ParseAPIError(resp.StatusCode(), resp.Body())
	}
	return result.Data.Items, nil
}

// GetDeploymentLogs returns the runtime logs for a deployment.
func (c *APIClient) GetDeploymentLogs(projectID, deploymentID string) (string, error) {
	var result Response[string]
	resp, err := c.Client.R().
		SetResult(&result).
		Get("/v1/projects/" + projectID + "/deployments/" + deploymentID + "/deployment-logs")
	if err != nil {
		return "", err
	}
	if resp.IsError() {
		return "", ParseAPIError(resp.StatusCode(), resp.Body())
	}
	return result.Data, nil
}

// BuildLogEntry represents a single build log line.
type BuildLogEntry struct {
	Log        string `json:"log"`
	Stage      string `json:"stage"`
	LineNumber int    `json:"lineNumber"`
	Timestamp  string `json:"ts"`
}

// GetDeploymentBuildLogs returns the build logs for a deployment.
func (c *APIClient) GetDeploymentBuildLogs(projectID, deploymentID string) ([]BuildLogEntry, error) {
	var result Response[[]BuildLogEntry]
	resp, err := c.Client.R().
		SetResult(&result).
		Get("/v1/projects/" + projectID + "/deployments/" + deploymentID + "/build-logs")
	if err != nil {
		return nil, err
	}
	if resp.IsError() {
		return nil, ParseAPIError(resp.StatusCode(), resp.Body())
	}
	return result.Data, nil
}

// RetriggerDeployment triggers a new deployment run and returns the new deployment.
// branch is optional — pass empty string to use the branch from the existing deployment.
func (c *APIClient) RetriggerDeployment(projectID, deploymentID, branch string) (*Deployment, error) {
	var result Response[Deployment]
	req := c.Client.R().SetResult(&result)
	if branch != "" {
		req = req.SetQueryParam("branch", branch)
	}
	resp, err := req.Post("/v1/projects/" + projectID + "/deployments/" + deploymentID + "/retrigger")
	if err != nil {
		return nil, err
	}
	if resp.IsError() {
		return nil, ParseAPIError(resp.StatusCode(), resp.Body())
	}
	return &result.Data, nil
}

// TriggerLatestDeployment triggers a new deployment from the latest commit.
// branch is optional — passed as a query param; omit to use the project's default branch.
func (c *APIClient) TriggerLatestDeployment(projectID, branch string) (*Deployment, error) {
	req := c.Client.R()
	if branch != "" {
		req = req.SetQueryParam("branch", branch)
	}
	resp, err := req.Post("/v1/projects/" + projectID + "/trigger-latest")
	if err != nil {
		return nil, err
	}
	if resp.IsError() {
		return nil, ParseAPIError(resp.StatusCode(), resp.Body())
	}

	// The API returns either {"data": {"id": "..."}} for a new deployment
	// or {"data": "deployment already triggered"} when the latest commit is already deployed.
	var envelope struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(resp.Body(), &envelope); err != nil {
		return nil, fmt.Errorf("unexpected response from server")
	}

	// Try to parse as an object with an ID
	var idResp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(envelope.Data, &idResp); err == nil && idResp.ID != "" {
		return c.GetDeployment(projectID, idResp.ID)
	}

	// Otherwise it's a plain string message (e.g. "deployment already triggered")
	var msg string
	if err := json.Unmarshal(envelope.Data, &msg); err == nil && msg != "" {
		return nil, fmt.Errorf("%s", msg)
	}

	return nil, fmt.Errorf("unexpected response from server")
}

// CancelDeployment cancels a running deployment.
func (c *APIClient) CancelDeployment(projectID, deploymentID string) error {
	resp, err := c.Client.R().
		Post("/v1/projects/" + projectID + "/deployments/" + deploymentID + "/cancel")
	if err != nil {
		return err
	}
	if resp.IsError() {
		return ParseAPIError(resp.StatusCode(), resp.Body())
	}
	return nil
}

// SleepDeployment puts a deployment to sleep (terminates it).
func (c *APIClient) SleepDeployment(projectID, deploymentID string) error {
	resp, err := c.Client.R().
		Delete("/v1/projects/" + projectID + "/deployments/" + deploymentID)
	if err != nil {
		return err
	}
	if resp.IsError() {
		return ParseAPIError(resp.StatusCode(), resp.Body())
	}
	return nil
}

// WakeupDeployment wakes up a sleeping deployment.
func (c *APIClient) WakeupDeployment(projectID, deploymentID string) error {
	resp, err := c.Client.R().
		Post("/v1/projects/" + projectID + "/deployments/" + deploymentID + "/wakeup")
	if err != nil {
		return err
	}
	if resp.IsError() {
		return ParseAPIError(resp.StatusCode(), resp.Body())
	}
	return nil
}

// DomainTXTRecord holds a TXT record for domain verification.
type DomainTXTRecord struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// DomainDNSRecords holds the DNS records required to set up a custom domain.
type DomainDNSRecords struct {
	TXTRecords []DomainTXTRecord `json:"txt_records"`
	ARecords   []string          `json:"a_records"`
}

// Domain represents a project custom domain.
type Domain struct {
	ID            string            `json:"id"`
	Name          string            `json:"name"`
	ProjectID     string            `json:"projectId"`
	EnvironmentID *string           `json:"environmentId"`
	Status        string            `json:"status"`
	Message       *string           `json:"message"`
	Records       *DomainDNSRecords `json:"records"`
	CreatedAt     string            `json:"createdAt"`
	UpdatedAt     string            `json:"updatedAt"`
}

// ListDomains returns all custom domains for a project.
func (c *APIClient) ListDomains(projectID string) ([]Domain, error) {
	var result Response[[]Domain]
	resp, err := c.Client.R().
		SetResult(&result).
		Get("/v1/projects/" + projectID + "/domains")
	if err != nil {
		return nil, err
	}
	if resp.IsError() {
		return nil, ParseAPIError(resp.StatusCode(), resp.Body())
	}
	return result.Data, nil
}

// AddDomain adds a custom domain to a project and returns the new domain ID.
// environmentID is optional — pass empty string to skip.
func (c *APIClient) AddDomain(projectID, name, environmentID string) (string, error) {
	var result Response[struct {
		ID string `json:"id"`
	}]
	body := map[string]any{"name": name}
	if environmentID != "" {
		body["environmentId"] = environmentID
	}
	resp, err := c.Client.R().
		SetResult(&result).
		SetBody(body).
		Post("/v1/projects/" + projectID + "/domains")
	if err != nil {
		return "", err
	}
	if resp.IsError() {
		return "", ParseAPIError(resp.StatusCode(), resp.Body())
	}
	return result.Data.ID, nil
}

// UpdateDomainEnvironment links a domain to an environment.
func (c *APIClient) UpdateDomainEnvironment(projectID, domainID, environmentID string) error {
	body := map[string]any{}
	if environmentID != "" {
		body["environmentId"] = environmentID
	}
	resp, err := c.Client.R().
		SetBody(body).
		Put("/v1/projects/" + projectID + "/domains/" + domainID + "/environment")
	if err != nil {
		return err
	}
	if resp.IsError() {
		return ParseAPIError(resp.StatusCode(), resp.Body())
	}
	return nil
}

// DeleteDomain removes a custom domain from a project.
func (c *APIClient) DeleteDomain(projectID, domainID string) error {
	resp, err := c.Client.R().
		Delete("/v1/projects/" + projectID + "/domains/" + domainID)
	if err != nil {
		return err
	}
	if resp.IsError() {
		return ParseAPIError(resp.StatusCode(), resp.Body())
	}
	return nil
}

// RefreshDomain refreshes the DNS status for a custom domain.
func (c *APIClient) RefreshDomain(projectID, domainID string) error {
	resp, err := c.Client.R().
		Post("/v1/projects/" + projectID + "/domains/" + domainID + "/refresh")
	if err != nil {
		return err
	}
	if resp.IsError() {
		return ParseAPIError(resp.StatusCode(), resp.Body())
	}
	return nil
}

// PurchaseSkillResponse holds the response after purchasing a skill.
type PurchaseSkillResponse struct {
	ID string `json:"id"`
}

// PurchaseSkill purchases a skill by skill ID and returns the purchase ID.
func (c *APIClient) PurchaseSkill(skillID string) (string, error) {
	var result Response[PurchaseSkillResponse]
	resp, err := c.Client.R().
		SetResult(&result).
		Post("/v1/skills/" + skillID + "/purchase")
	if err != nil {
		return "", err
	}
	if resp.IsError() {
		return "", ParseAPIError(resp.StatusCode(), resp.Body())
	}
	return result.Data.ID, nil
}

// EnvironmentExtra holds extra environment metadata.
type EnvironmentExtra struct {
	Endpoint      string   `json:"endpoint"`
	CustomDomains []string `json:"customDomains"`
}

// EnvironmentSettings holds mutable environment settings returned by the API.
type EnvironmentSettings struct {
	RunEnvs map[string]string `json:"runEnvs"`
}

// Environment represents a project environment.
type Environment struct {
	ID                   string              `json:"id"`
	DisplayName          string              `json:"displayName"`
	UniqueName           string              `json:"uniqueName"`
	Description          *string             `json:"description"`
	ProjectID            string              `json:"projectId"`
	Branch               *string             `json:"branch"`
	ProjectDeploymentID  *string             `json:"projectDeploymentId"`
	IsAutoPromoteEnabled bool                `json:"isAutoPromoteEnabled"`
	Status               string              `json:"status"`
	CreatedAt            time.Time           `json:"createdAt"`
	UpdatedAt            time.Time           `json:"updatedAt"`
	Settings             EnvironmentSettings `json:"settings"`
	Extra                EnvironmentExtra    `json:"extra"`
	Resources            ResourceSettings    `json:"resources"`
}

// ListEnvironments returns all environments for a project.
func (c *APIClient) ListEnvironments(projectID string) ([]Environment, error) {
	var result Response[[]Environment]
	resp, err := c.Client.R().
		SetResult(&result).
		Get("/v1/projects/" + projectID + "/environments")
	if err != nil {
		return nil, err
	}
	if resp.IsError() {
		return nil, ParseAPIError(resp.StatusCode(), resp.Body())
	}
	return result.Data, nil
}

// CreateEnvironmentRequest is the request body for creating a new environment.
type CreateEnvironmentRequest struct {
	DisplayName          string            `json:"displayName"`
	UniqueName           string            `json:"uniqueName"`
	Description          *string           `json:"description,omitempty"`
	Branch               *string           `json:"branch,omitempty"`
	Settings             map[string]any    `json:"settings"`
	Resources            ResourceSettings  `json:"resources"`
	IsAutoPromoteEnabled bool              `json:"isAutoPromoteEnabled"`
}

// CreateEnvironment creates a new environment for a project.
func (c *APIClient) CreateEnvironment(projectID string, req CreateEnvironmentRequest) (*Environment, error) {
	var result Response[Environment]
	resp, err := c.Client.R().
		SetBody(req).
		SetResult(&result).
		Post("/v1/projects/" + projectID + "/environments")
	if err != nil {
		return nil, err
	}
	if resp.IsError() {
		return nil, ParseAPIError(resp.StatusCode(), resp.Body())
	}
	return &result.Data, nil
}

// PromoteDeployment assigns a deployment to an environment.
func (c *APIClient) PromoteDeployment(projectID, environmentID, deploymentID string) error {
	resp, err := c.Client.R().
		SetBody(map[string]string{"deploymentId": deploymentID}).
		Post("/v1/projects/" + projectID + "/environments/" + environmentID + "/assign-deployment")
	if err != nil {
		return err
	}
	if resp.IsError() {
		return ParseAPIError(resp.StatusCode(), resp.Body())
	}
	return nil
}

// DeleteEnvironment deletes an environment from a project.
func (c *APIClient) DeleteEnvironment(projectID, environmentID string) error {
	resp, err := c.Client.R().
		Delete("/v1/projects/" + projectID + "/environments/" + environmentID)
	if err != nil {
		return err
	}
	if resp.IsError() {
		return ParseAPIError(resp.StatusCode(), resp.Body())
	}
	return nil
}

// GetEnvironmentVariables returns the environment variables for a project environment.
func (c *APIClient) GetEnvironmentVariables(projectID, environmentID string) (map[string]string, error) {
	environments, err := c.ListEnvironments(projectID)
	if err != nil {
		return nil, err
	}
	for _, env := range environments {
		if env.ID == environmentID {
			if env.Settings.RunEnvs == nil {
				return map[string]string{}, nil
			}
			return env.Settings.RunEnvs, nil
		}
	}
	return nil, &APIError{StatusCode: 404, Message: "environment not found"}
}

// UpdateEnvironmentVariables sets the environment variables for a project environment.
func (c *APIClient) UpdateEnvironmentVariables(projectID, environmentID string, vars map[string]string) error {
	resp, err := c.Client.R().
		SetBody(map[string]any{"runEnvs": vars}).
		Put("/v1/projects/" + projectID + "/environments/" + environmentID + "/environment-variables")
	if err != nil {
		return err
	}
	if resp.IsError() {
		return ParseAPIError(resp.StatusCode(), resp.Body())
	}
	return nil
}

// ResourceSettings represents the resource allocation for an environment.
type ResourceSettings struct {
	Replicas int `json:"replicas"`
	CPU      int `json:"cpu"`
	Memory   int `json:"memory"`
}

// ScaleRequest is the request body for updating environment resources.
type ScaleRequest = ResourceSettings

// GetEnvironmentResources returns the resource settings for an environment.
// Resources are embedded in the environment object — there is no separate GET endpoint.
func (c *APIClient) GetEnvironmentResources(projectID, environmentID string) (*ResourceSettings, error) {
	envs, err := c.ListEnvironments(projectID)
	if err != nil {
		return nil, err
	}
	for _, env := range envs {
		if env.ID == environmentID {
			return &env.Resources, nil
		}
	}
	return nil, fmt.Errorf("environment %q not found", environmentID)
}

// UpdateEnvironmentResources updates the resource allocation for an environment.
func (c *APIClient) UpdateEnvironmentResources(projectID, environmentID string, req ScaleRequest) error {
	resp, err := c.Client.R().
		SetBody(req).
		Put("/v1/projects/" + projectID + "/environments/" + environmentID + "/resources")
	if err != nil {
		return err
	}
	if resp.IsError() {
		return ParseAPIError(resp.StatusCode(), resp.Body())
	}
	return nil
}

// ProjectTemplate represents a published project template.
type ProjectTemplate struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Status      string    `json:"status"`
	Categories  []string  `json:"categories"`
	Amount      float64   `json:"amount"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// listTemplatesData is the shape of data returned by GET /v1/project-templates.
type listTemplatesData struct {
	Templates []ProjectTemplate `json:"templates"`
}

// ListPublishedTemplates returns all published project templates.
func (c *APIClient) ListPublishedTemplates() ([]ProjectTemplate, error) {
	var result Response[listTemplatesData]
	resp, err := c.Client.R().
		SetResult(&result).
		Get("/v1/project-templates")
	if err != nil {
		return nil, err
	}
	if resp.IsError() {
		return nil, ParseAPIError(resp.StatusCode(), resp.Body())
	}
	return result.Data.Templates, nil
}

// GetTemplate returns a project template by ID.
func (c *APIClient) GetTemplate(id string) (*ProjectTemplate, error) {
	var result Response[ProjectTemplate]
	resp, err := c.Client.R().
		SetResult(&result).
		Get("/v1/project-templates/" + id)
	if err != nil {
		return nil, err
	}
	if resp.IsError() {
		return nil, ParseAPIError(resp.StatusCode(), resp.Body())
	}
	return &result.Data, nil
}

// TemplatePurchase represents a user's purchase of a project template.
type TemplatePurchase struct {
	ID                string    `json:"id"`
	ProjectTemplateID string    `json:"projectTemplateId"`
	CreatedAt         time.Time `json:"createdAt"`
}

// listPurchasesData is the shape of data returned by GET /v1/project-templates/purchases.
type listPurchasesData struct {
	Purchases []TemplatePurchase `json:"purchases"`
}

// BuyTemplate purchases a template and returns the purchase ID.
func (c *APIClient) BuyTemplate(templateID string) (string, error) {
	var result Response[struct {
		ID string `json:"id"`
	}]
	resp, err := c.Client.R().
		SetResult(&result).
		Post("/v1/project-templates/" + templateID + "/buy")
	if err != nil {
		return "", err
	}
	if resp.IsError() {
		return "", ParseAPIError(resp.StatusCode(), resp.Body())
	}
	return result.Data.ID, nil
}

// ListTemplatePurchases returns all of the user's template purchases.
func (c *APIClient) ListTemplatePurchases() ([]TemplatePurchase, error) {
	var result Response[listPurchasesData]
	resp, err := c.Client.R().
		SetResult(&result).
		SetQueryParam("limit", "50").
		Get("/v1/project-templates/purchases")
	if err != nil {
		return nil, err
	}
	if resp.IsError() {
		return nil, ParseAPIError(resp.StatusCode(), resp.Body())
	}
	return result.Data.Purchases, nil
}

// GetTemplatePurchaseDownloadURL returns a signed download URL for a purchase.
func (c *APIClient) GetTemplatePurchaseDownloadURL(purchaseID string) (string, error) {
	var result Response[struct {
		DownloadURI string `json:"downloadUri"`
	}]
	resp, err := c.Client.R().
		SetResult(&result).
		Get("/v1/project-templates/purchases/" + purchaseID + "/download")
	if err != nil {
		return "", err
	}
	if resp.IsError() {
		return "", ParseAPIError(resp.StatusCode(), resp.Body())
	}
	return result.Data.DownloadURI, nil
}

// CreateDeployment creates a new deployment for an image-type project.
func (c *APIClient) CreateDeployment(projectID string, body map[string]any) (*Deployment, error) {
	var result Response[Deployment]
	resp, err := c.Client.R().
		SetResult(&result).
		SetBody(body).
		Post("/v1/projects/" + projectID + "/deployments")
	if err != nil {
		return nil, err
	}
	if resp.IsError() {
		return nil, ParseAPIError(resp.StatusCode(), resp.Body())
	}
	return &result.Data, nil
}

// UploadDeploymentZip creates a new deployment by uploading a ZIP file.
// Only available for upload-type projects.
func (c *APIClient) UploadDeploymentZip(projectID, zipPath string) (*Deployment, error) {
	var result Response[Deployment]
	resp, err := c.Client.R().
		SetResult(&result).
		SetFile("file", zipPath).
		Put("/v1/projects/" + projectID + "/deployments/upload-zip")
	if err != nil {
		return nil, err
	}
	if resp.IsError() {
		return nil, ParseAPIError(resp.StatusCode(), resp.Body())
	}
	return &result.Data, nil
}

// GetDeployment returns a single deployment by ID.
func (c *APIClient) GetDeployment(projectID, deploymentID string) (*Deployment, error) {
	var result Response[Deployment]
	resp, err := c.Client.R().
		SetResult(&result).
		Get("/v1/projects/" + projectID + "/deployments/" + deploymentID)
	if err != nil {
		return nil, err
	}
	if resp.IsError() {
		return nil, ParseAPIError(resp.StatusCode(), resp.Body())
	}
	return &result.Data, nil
}
