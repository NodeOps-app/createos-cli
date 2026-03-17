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

func (c *ApiClient) ListProjects() ([]Project, error) {
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

func (c *ApiClient) GetProject(id string) (*Project, error) {
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

func (c *ApiClient) DeleteProject(id string) error {
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

func (c *ApiClient) GetUser() (*User, error) {
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

type Skill struct {
	Id         string    `json:"id"`
	UserId     string    `json:"userId"`
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

type PurchasedSkillItem struct {
	Id              string    `json:"id"`
	SkillId         string    `json:"skillId"`
	UserId          string    `json:"userId"`
	PurchasedAmount float64   `json:"purchasedAmount"`
	CreatedAt       time.Time `json:"createdAt"`
	Skill           Skill     `json:"skill,omitempty"`
}

func (c *ApiClient) ListMyPurchasedSkills() ([]PurchasedSkillItem, error) {
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

type GetDownloadUrl struct {
	DownloadUrl string `json:"downloadUri"`
}

func (c *ApiClient) GetSkillDownloadUrl(purchasedId string) (string, error) {
	var result Response[GetDownloadUrl]
	resp, err := c.Client.R().
		SetResult(&result).
		Get("/v1/skills/purchased/" + purchasedId + "/download")
	if err != nil {
		return "", err
	}

	if resp.IsError() {
		return "", ParseAPIError(resp.StatusCode(), resp.Body())
	}

	return result.Data.DownloadUrl, nil
}

type PaginatedResponse[T any] struct {
	Data struct {
		Items []T `json:"data"`
	} `json:"data"`
	Pagination Pagination `json:"pagination"`
}

type Pagination struct {
	Total  int `json:"total"`
	Limit  int `json:"limit"`
	Offset int `json:"offset"`
	Count  int `json:"count"`
}

func (c *ApiClient) ListAvailableSkillsForPurchase(searchText string, offset int, limit int) ([]Skill, Pagination, error) {
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

// Deployment represents a project deployment
type DeploymentExtra struct {
	Endpoint string `json:"endpoint"`
}

type Deployment struct {
	ID            string          `json:"id"`
	ProjectID     string          `json:"projectId"`
	Status        string          `json:"status"`
	VersionNumber int             `json:"versionNumber"`
	Extra         DeploymentExtra `json:"extra"`
	CreatedAt     time.Time       `json:"createdAt"`
	UpdatedAt     time.Time       `json:"updatedAt"`
}

func (c *ApiClient) ListDeployments(projectID string) ([]Deployment, error) {
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

func (c *ApiClient) GetDeploymentLogs(projectID, deploymentID string) (string, error) {
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

type BuildLogEntry struct {
	Log        string `json:"log"`
	Stage      string `json:"stage"`
	LineNumber int    `json:"lineNumber"`
	Timestamp  string `json:"ts"`
}

func (c *ApiClient) GetDeploymentBuildLogs(projectID, deploymentID string) ([]BuildLogEntry, error) {
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

func (c *ApiClient) RetriggerDeployment(projectID, deploymentID string) error {
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

func (c *ApiClient) CancelDeployment(projectID, deploymentID string) error {
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

func (c *ApiClient) WakeupDeployment(projectID, deploymentID string) error {
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

func (c *ApiClient) ListDomains(projectID string) ([]Domain, error) {
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

func (c *ApiClient) AddDomain(projectID, name string) (string, error) {
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

func (c *ApiClient) DeleteDomain(projectID, domainID string) error {
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

func (c *ApiClient) RefreshDomain(projectID, domainID string) error {
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

type PurchaseSkillResponse struct {
	Id string `json:"id"`
}

func (c *ApiClient) PurchaseSkill(skillId string) (string, error) {
	var result Response[PurchaseSkillResponse]
	resp, err := c.Client.R().
		SetResult(&result).
		Post("/v1/skills/" + skillId + "/purchase")
	if err != nil {
		return "", err
	}
	if resp.IsError() {
		return "", ParseAPIError(resp.StatusCode(), resp.Body())
	}
	return result.Data.Id, nil
}

// Environment represents a project environment
type EnvironmentExtra struct {
	Endpoint      string   `json:"endpoint"`
	CustomDomains []string `json:"customDomains"`
}

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

func (c *ApiClient) ListEnvironments(projectID string) ([]Environment, error) {
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

func (c *ApiClient) DeleteEnvironment(projectID, environmentID string) error {
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
