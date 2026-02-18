package api

import (
	"fmt"
	"time"
)

func (c *ApiClient) GetUser() (*User, error) {
	var result Response[User]
	resp, err := c.Client.R().
		SetResult(&result).
		Get("/v1/users/me")
	if err != nil {
		return nil, err
	}

	if resp.IsError() {
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode(), resp.String())
	}

	return &result.Data, nil
}

type Skill struct {
	Id         string    `json:"id"`
	UserId     string    `json:"userId"`
	Name       string    `json:"name"`
	UniqueName string    `json:"uniqueName"`
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
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode(), resp.String())
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
		return "", fmt.Errorf("API error %d: %s", resp.StatusCode(), resp.String())
	}

	return result.Data.DownloadUrl, nil
}
