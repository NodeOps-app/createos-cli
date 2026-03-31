package api

import (
	"encoding/json"
	"time"
)

// Cronjob represents a scheduled cron job attached to a project environment.
type Cronjob struct {
	ID                      string           `json:"id"`
	Name                    string           `json:"name"`
	Schedule                string           `json:"schedule"`
	Type                    string           `json:"type"`
	Status                  string           `json:"status"`
	EnvironmentID           string           `json:"environmentId"`
	ProjectID               string           `json:"projectId"`
	Metadata                *json.RawMessage `json:"metadata"`
	Settings                *json.RawMessage `json:"settings"`
	SuspendedAt             *time.Time       `json:"suspendedAt"`
	SuspendedByOwner        *bool            `json:"suspendedByOwner"`
	SuspendText             *string          `json:"suspendText"`
	UserRequestTerminatedAt *time.Time       `json:"userRequestTerminatedAt"`
	CreatedAt               time.Time        `json:"createdAt"`
	UpdatedAt               time.Time        `json:"updatedAt"`
}

// CronjobActivity represents a single execution record for a cron job.
type CronjobActivity struct {
	ID          string    `json:"id"`
	CronJobID   string    `json:"cronJobId"`
	Log         *string   `json:"log"`
	Success     *bool     `json:"success"`
	StatusCode  *int      `json:"statusCode"`
	ScheduledAt time.Time `json:"scheduledAt"`
	CreatedAt   time.Time `json:"createdAt"`
}

// CreateCronjobRequest is the request body for creating a new cron job.
type CreateCronjobRequest struct {
	Name          string          `json:"name"`
	Schedule      string          `json:"schedule"`
	Type          string          `json:"type"`
	EnvironmentID string          `json:"environmentId"`
	Settings      json.RawMessage `json:"settings,omitempty"`
}

// HTTPCronjobSettings holds the settings for an HTTP-type cron job.
type HTTPCronjobSettings struct {
	Path             string            `json:"path"`
	Method           string            `json:"method"`
	Headers          map[string]string `json:"headers,omitempty"`
	TimeoutInSeconds *int              `json:"timeoutInSeconds,omitempty"`
}

// UpdateCronjobRequest is the request body for updating a cron job.
type UpdateCronjobRequest struct {
	Name     string          `json:"name"`
	Schedule string          `json:"schedule"`
	Settings json.RawMessage `json:"settings,omitempty"`
}

// CreateCronjobResponse holds the ID returned after creating a cron job.
type CreateCronjobResponse struct {
	ID string `json:"id"`
}

// ListCronjobs returns all cron jobs for a project.
func (c *APIClient) ListCronjobs(projectID string) ([]Cronjob, error) {
	var result Response[[]Cronjob]
	resp, err := c.Client.R().
		SetResult(&result).
		Get("/v1/projects/" + projectID + "/cronjobs")
	if err != nil {
		return nil, err
	}
	if resp.IsError() {
		return nil, ParseAPIError(resp.StatusCode(), resp.Body())
	}
	return result.Data, nil
}

// GetCronjob returns a single cron job by ID.
func (c *APIClient) GetCronjob(projectID, cronjobID string) (*Cronjob, error) {
	var result Response[Cronjob]
	resp, err := c.Client.R().
		SetResult(&result).
		Get("/v1/projects/" + projectID + "/cronjobs/" + cronjobID)
	if err != nil {
		return nil, err
	}
	if resp.IsError() {
		return nil, ParseAPIError(resp.StatusCode(), resp.Body())
	}
	return &result.Data, nil
}

// CreateCronjob creates a new cron job for a project and returns its ID.
func (c *APIClient) CreateCronjob(projectID string, req CreateCronjobRequest) (string, error) {
	var result Response[CreateCronjobResponse]
	resp, err := c.Client.R().
		SetResult(&result).
		SetBody(req).
		Post("/v1/projects/" + projectID + "/cronjobs")
	if err != nil {
		return "", err
	}
	if resp.IsError() {
		return "", ParseAPIError(resp.StatusCode(), resp.Body())
	}
	return result.Data.ID, nil
}

// UpdateCronjob updates the name, schedule, and settings of a cron job.
func (c *APIClient) UpdateCronjob(projectID, cronjobID string, req UpdateCronjobRequest) error {
	resp, err := c.Client.R().
		SetBody(req).
		Put("/v1/projects/" + projectID + "/cronjobs/" + cronjobID)
	if err != nil {
		return err
	}
	if resp.IsError() {
		return ParseAPIError(resp.StatusCode(), resp.Body())
	}
	return nil
}

// DeleteCronjob deletes a cron job by ID.
func (c *APIClient) DeleteCronjob(projectID, cronjobID string) error {
	resp, err := c.Client.R().
		Delete("/v1/projects/" + projectID + "/cronjobs/" + cronjobID)
	if err != nil {
		return err
	}
	if resp.IsError() {
		return ParseAPIError(resp.StatusCode(), resp.Body())
	}
	return nil
}

// SuspendCronjob suspends a cron job, stopping it from running on schedule.
func (c *APIClient) SuspendCronjob(projectID, cronjobID string) error {
	resp, err := c.Client.R().
		Post("/v1/projects/" + projectID + "/cronjobs/" + cronjobID + "/suspend")
	if err != nil {
		return err
	}
	if resp.IsError() {
		return ParseAPIError(resp.StatusCode(), resp.Body())
	}
	return nil
}

// UnsuspendCronjob resumes a suspended cron job.
func (c *APIClient) UnsuspendCronjob(projectID, cronjobID string) error {
	resp, err := c.Client.R().
		Post("/v1/projects/" + projectID + "/cronjobs/" + cronjobID + "/unsuspend")
	if err != nil {
		return err
	}
	if resp.IsError() {
		return ParseAPIError(resp.StatusCode(), resp.Body())
	}
	return nil
}

// ListCronjobActivities returns the recent execution history for a cron job.
func (c *APIClient) ListCronjobActivities(projectID, cronjobID string) ([]CronjobActivity, error) {
	var result Response[[]CronjobActivity]
	resp, err := c.Client.R().
		SetResult(&result).
		Get("/v1/projects/" + projectID + "/cronjobs/" + cronjobID + "/activities")
	if err != nil {
		return nil, err
	}
	if resp.IsError() {
		return nil, ParseAPIError(resp.StatusCode(), resp.Body())
	}
	return result.Data, nil
}
