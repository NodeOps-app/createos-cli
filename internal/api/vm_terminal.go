package api

import (
	"encoding/json"
	"fmt"
	"time"
)

// VMProductID is the product ID for the vm-terminal product.
const VMProductID = "972a0414-3a75-42b9-9a11-7b410b32cab7"

// VMSize represents a VM size option returned by the products API.
type VMSize struct {
	Name            string
	CPU             int64 // millicores
	MemoryMiB       int64
	DiskMiB         int64
	BandwidthMiB    int64
	CreationEnabled bool
}

// vmSizeResource is the raw resource shape from GET /v1/products.
type vmSizeResource struct {
	CPU             int64 `json:"cpuInMillicores"`
	MemoryMiB       int64 `json:"memoryInMiB"`
	DiskMiB         int64 `json:"diskInMiB"`
	BandwidthMiB    int64 `json:"bandwidthInMiB"`
	CreationEnabled bool  `json:"creationEnabled"`
}

// vmProduct is the shape of one entry in the products list response.
type vmProduct struct {
	ID         string           `json:"id"`
	UniqueName string           `json:"uniqueName"`
	Resources  []vmSizeResource `json:"resources"`
}

// vmProductsResponse is the top-level response from GET /v1/products.
type vmProductsResponse struct {
	Products []vmProduct `json:"products"`
}

// GetVMSizes fetches available VM sizes for the vm-terminal product from the products API.
func (c *APIClient) GetVMSizes() ([]VMSize, error) {
	var result Response[vmProductsResponse]
	resp, err := c.Client.R().
		SetResult(&result).
		Get("/v1/products")
	if err != nil {
		return nil, err
	}
	if resp.IsError() {
		return nil, ParseAPIError(resp.StatusCode(), resp.Body())
	}
	for _, p := range result.Data.Products {
		if p.ID == VMProductID {
			sizes := make([]VMSize, 0, len(p.Resources))
			for _, r := range p.Resources {
				if r.CreationEnabled {
					sizes = append(sizes, VMSize{
						CPU:             r.CPU,
						MemoryMiB:       r.MemoryMiB,
						DiskMiB:         r.DiskMiB,
						BandwidthMiB:    r.BandwidthMiB,
						CreationEnabled: r.CreationEnabled,
					})
				}
			}
			return sizes, nil
		}
	}
	return nil, fmt.Errorf("vm-terminal product not found")
}

// VMDeploymentExtra holds extra VM deployment metadata.
type VMDeploymentExtra struct {
	IPAddress string `json:"ipAddress"`
}

// VMFirewallRule represents a firewall rule on a VM deployment.
type VMFirewallRule struct {
	Port  int    `json:"port"`
	Proto string `json:"proto"`
	From  string `json:"from"`
}

// VMDeploymentInputs holds the mutable inputs of a VM deployment.
type VMDeploymentInputs struct {
	SSHKeys       []string         `json:"SSH_KEYS"`
	FirewallRules []VMFirewallRule `json:"FIREWALL_RULES"`
}

// VMDeployment represents a VM terminal deployment.
type VMDeployment struct {
	ID        string             `json:"id"`
	Name      *string            `json:"name"`
	Status    string             `json:"status"` // deploying|deployed|resizing|terminating|terminated
	Extra     VMDeploymentExtra  `json:"extra"`
	Inputs    VMDeploymentInputs `json:"inputs"`
	CreatedAt time.Time          `json:"createdAt"`
	UpdatedAt time.Time          `json:"updatedAt"`
}

// vmResourceInputs is the resource spec for VM creation/resize.
type vmResourceInputs struct {
	CPU      int64 `json:"cpuInMillicores"`
	MemoryMiB int64 `json:"memoryInMB"`
	DiskMiB  int64 `json:"storageInMB"`
}

// vmCreateInputs is the product-specific inputs for vm-terminal.
type vmCreateInputs struct {
	Zone          string           `json:"ZONE"`
	Provider      string           `json:"PROVIDER"`
	SSHKeys       []string         `json:"SSH_KEYS"`
	FirewallRules []VMFirewallRule  `json:"FIREWALL_RULES"`
}

// vmUpdateInputs is the mutable inputs for PUT /v1/product-deployments/:id.
type vmUpdateInputs struct {
	SSHKeys       []string         `json:"SSH_KEYS"`
	FirewallRules []VMFirewallRule  `json:"FIREWALL_RULES"`
}

// updateVMRequest is the request body for PUT /v1/product-deployments/:id.
type updateVMRequest struct {
	Inputs vmUpdateInputs `json:"inputs"`
}

// createVMRequest is the request body for POST /v1/product-deployments.
type createVMRequest struct {
	Name           *string          `json:"name,omitempty"`
	ProductID      string           `json:"productId"`
	Inputs         json.RawMessage  `json:"inputs"`
	ResourceInputs vmResourceInputs `json:"resourceInputs"`
}

// resizeVMRequest is the request body for POST /v1/product-deployments/:id/resize.
type resizeVMRequest struct {
	ResourceInputs vmResourceInputs `json:"resourceInputs"`
}

// DOZone represents a DigitalOcean availability zone.
type DOZone struct {
	Name    string `json:"name"`
	Region  string `json:"region"`
	Country string `json:"country"`
}

// GetDOZones returns all available DigitalOcean zones.
func (c *APIClient) GetDOZones() ([]DOZone, error) {
	var result Response[[]DOZone]
	resp, err := c.Client.R().
		SetResult(&result).
		Get("/v1/databases/providers/do/zones")
	if err != nil {
		return nil, err
	}
	if resp.IsError() {
		return nil, ParseAPIError(resp.StatusCode(), resp.Body())
	}
	return result.Data, nil
}

// CreateVMDeployment creates a new VM terminal deployment.
func (c *APIClient) CreateVMDeployment(name, zone string, sshKeys []string, size VMSize) (*VMDeployment, error) {
	if sshKeys == nil {
		sshKeys = []string{}
	}
	inputs, err := json.Marshal(vmCreateInputs{
		Zone:          zone,
		Provider:      "do",
		SSHKeys:       sshKeys,
		FirewallRules: []VMFirewallRule{},
	})
	if err != nil {
		return nil, err
	}

	var namePtr *string
	if name != "" {
		namePtr = &name
	}

	req := createVMRequest{
		Name:      namePtr,
		ProductID: VMProductID,
		Inputs:    json.RawMessage(inputs),
		ResourceInputs: vmResourceInputs{
			CPU:       size.CPU,
			MemoryMiB: size.MemoryMiB,
			DiskMiB:   size.DiskMiB,
		},
	}

	var result Response[VMDeployment]
	resp, err := c.Client.R().
		SetResult(&result).
		SetBody(req).
		Post("/v1/product-deployments")
	if err != nil {
		return nil, err
	}
	if resp.IsError() {
		return nil, ParseAPIError(resp.StatusCode(), resp.Body())
	}
	return &result.Data, nil
}

// ListVMDeployments returns all VM terminal deployments for the user.
func (c *APIClient) ListVMDeployments() ([]VMDeployment, error) {
	var result PaginatedResponse[VMDeployment]
	resp, err := c.Client.R().
		SetResult(&result).
		SetQueryParam("productId", VMProductID).
		Get("/v1/product-deployments")
	if err != nil {
		return nil, err
	}
	if resp.IsError() {
		return nil, ParseAPIError(resp.StatusCode(), resp.Body())
	}
	return result.Data.Items, nil
}

// GetVMDeployment returns a single VM deployment by ID.
func (c *APIClient) GetVMDeployment(id string) (*VMDeployment, error) {
	var result Response[VMDeployment]
	resp, err := c.Client.R().
		SetResult(&result).
		Get("/v1/product-deployments/" + id)
	if err != nil {
		return nil, err
	}
	if resp.IsError() {
		return nil, ParseAPIError(resp.StatusCode(), resp.Body())
	}
	return &result.Data, nil
}

// ResizeVMDeployment resizes a VM deployment to a new size.
func (c *APIClient) ResizeVMDeployment(id string, size VMSize) error {
	req := resizeVMRequest{
		ResourceInputs: vmResourceInputs{
			CPU:       size.CPU,
			MemoryMiB: size.MemoryMiB,
			DiskMiB:   size.DiskMiB,
		},
	}
	resp, err := c.Client.R().
		SetBody(req).
		Post("/v1/product-deployments/" + id + "/resize")
	if err != nil {
		return err
	}
	if resp.IsError() {
		return ParseAPIError(resp.StatusCode(), resp.Body())
	}
	return nil
}

// RebootVMDeployment reboots a VM deployment.
func (c *APIClient) RebootVMDeployment(id string) error {
	resp, err := c.Client.R().
		Post("/v1/product-deployments/" + id + "/reboot")
	if err != nil {
		return err
	}
	if resp.IsError() {
		return ParseAPIError(resp.StatusCode(), resp.Body())
	}
	return nil
}

// UpdateVMDeployment updates the mutable inputs (SSH keys, firewall rules) of a VM deployment.
func (c *APIClient) UpdateVMDeployment(id string, sshKeys []string, firewallRules []VMFirewallRule) error {
	if sshKeys == nil {
		sshKeys = []string{}
	}
	if firewallRules == nil {
		firewallRules = []VMFirewallRule{}
	}
	req := updateVMRequest{
		Inputs: vmUpdateInputs{
			SSHKeys:       sshKeys,
			FirewallRules: firewallRules,
		},
	}
	resp, err := c.Client.R().
		SetBody(req).
		Put("/v1/product-deployments/" + id)
	if err != nil {
		return err
	}
	if resp.IsError() {
		return ParseAPIError(resp.StatusCode(), resp.Body())
	}
	return nil
}

// TerminateVMDeployment terminates (deletes) a VM deployment.
func (c *APIClient) TerminateVMDeployment(id string) error {
	resp, err := c.Client.R().
		Delete("/v1/product-deployments/" + id)
	if err != nil {
		return err
	}
	if resp.IsError() {
		return ParseAPIError(resp.StatusCode(), resp.Body())
	}
	return nil
}
