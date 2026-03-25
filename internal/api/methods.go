package api

import (
	"strconv"
	"time"
)

// Project represents a CreateOS project
type Project struct {
	ID          string    `json:"id"`
	UniqueName  string    `json:"uniqueName"`
	DisplayName string    `json:"displayName"`
	Description *string   `json:"description"`
	Status      string    `json:"status"`
	Type        string    `json:"type"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
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
		Items []T `json:"data"`
	} `json:"data"`
	Pagination Pagination `json:"pagination"`
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
	return result.Data.Items, result.Pagination, nil
}

// DeploymentExtra holds extra deployment metadata.
type DeploymentExtra struct {
	Endpoint string `json:"endpoint"`
}

// Deployment represents a project deployment.
type Deployment struct {
	ID            string          `json:"id"`
	ProjectID     string          `json:"projectId"`
	Status        string          `json:"status"`
	VersionNumber int             `json:"versionNumber"`
	Extra         DeploymentExtra `json:"extra"`
	CreatedAt     time.Time       `json:"createdAt"`
	UpdatedAt     time.Time       `json:"updatedAt"`
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

// RetriggerDeployment triggers a new deployment run.
func (c *APIClient) RetriggerDeployment(projectID, deploymentID string) error {
	resp, err := c.Client.R().
		Post("/v1/projects/" + projectID + "/deployments/" + deploymentID + "/retrigger")
	if err != nil {
		return err
	}
	if resp.IsError() {
		return ParseAPIError(resp.StatusCode(), resp.Body())
	}
	return nil
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

// Domain represents a project custom domain
type Domain struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	ProjectID string  `json:"projectId"`
	Status    string  `json:"status"`
	Message   *string `json:"message"`
	CreatedAt string  `json:"createdAt"`
	UpdatedAt string  `json:"updatedAt"`
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
func (c *APIClient) AddDomain(projectID, name string) (string, error) {
	var result Response[struct {
		ID string `json:"id"`
	}]
	resp, err := c.Client.R().
		SetResult(&result).
		SetBody(map[string]string{"name": name}).
		Post("/v1/projects/" + projectID + "/domains")
	if err != nil {
		return "", err
	}
	if resp.IsError() {
		return "", ParseAPIError(resp.StatusCode(), resp.Body())
	}
	return result.Data.ID, nil
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

// Environment represents a project environment.
type Environment struct {
	ID                   string           `json:"id"`
	DisplayName          string           `json:"displayName"`
	UniqueName           string           `json:"uniqueName"`
	Description          *string          `json:"description"`
	ProjectID            string           `json:"projectId"`
	Branch               *string          `json:"branch"`
	ProjectDeploymentID  *string          `json:"projectDeploymentId"`
	IsAutoPromoteEnabled bool             `json:"isAutoPromoteEnabled"`
	Status               string           `json:"status"`
	CreatedAt            time.Time        `json:"createdAt"`
	UpdatedAt            time.Time        `json:"updatedAt"`
	Extra                EnvironmentExtra `json:"extra"`
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
	var result Response[map[string]string]
	resp, err := c.Client.R().
		SetResult(&result).
		Get("/v1/projects/" + projectID + "/environments/" + environmentID + "/environment-variables")
	if err != nil {
		return nil, err
	}
	if resp.IsError() {
		return nil, ParseAPIError(resp.StatusCode(), resp.Body())
	}
	return result.Data, nil
}

// UpdateEnvironmentVariables sets the environment variables for a project environment.
func (c *APIClient) UpdateEnvironmentVariables(projectID, environmentID string, vars map[string]string) error {
	resp, err := c.Client.R().
		SetBody(map[string]any{"environmentVariables": vars}).
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
func (c *APIClient) GetEnvironmentResources(projectID, environmentID string) (*ResourceSettings, error) {
	var result Response[ResourceSettings]
	resp, err := c.Client.R().
		SetResult(&result).
		Get("/v1/projects/" + projectID + "/environments/" + environmentID + "/resources")
	if err != nil {
		return nil, err
	}
	if resp.IsError() {
		return nil, ParseAPIError(resp.StatusCode(), resp.Body())
	}
	return &result.Data, nil
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
	Description *string   `json:"description"`
	Type        string    `json:"type"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// ListPublishedTemplates returns all published project templates.
func (c *APIClient) ListPublishedTemplates() ([]ProjectTemplate, error) {
	var result PaginatedResponse[ProjectTemplate]
	resp, err := c.Client.R().
		SetResult(&result).
		Get("/v1/project-templates/published")
	if err != nil {
		return nil, err
	}
	if resp.IsError() {
		return nil, ParseAPIError(resp.StatusCode(), resp.Body())
	}
	return result.Data.Items, nil
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

// GetTemplateDownloadURL returns the download URL for a project template.
func (c *APIClient) GetTemplateDownloadURL(id string) (string, error) {
	var result Response[struct {
		DownloadURL string `json:"downloadUri"`
	}]
	resp, err := c.Client.R().
		SetResult(&result).
		Get("/v1/project-templates/" + id + "/download")
	if err != nil {
		return "", err
	}
	if resp.IsError() {
		return "", ParseAPIError(resp.StatusCode(), resp.Body())
	}
	return result.Data.DownloadURL, nil
}

// CreateDeployment creates a new deployment for a project.
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
