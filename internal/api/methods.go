package api

import (
	"fmt"
	"strconv"
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

type PaginatedResponse[T any] struct {
	Data struct {
		Skills []T `json:"data"`
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
		return nil, Pagination{}, fmt.Errorf("API error %d: %s", resp.StatusCode(), resp.String())
	}
	return result.Data.Skills, result.Pagination, nil
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
		return "", fmt.Errorf("API error %d: %s", resp.StatusCode(), resp.String())
	}
	return result.Data.Id, nil
}
