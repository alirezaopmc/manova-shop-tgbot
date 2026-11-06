package hetzner

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/alirezaopmc/manova-shop-tgbot/internal/application/port"
	"github.com/alirezaopmc/manova-shop-tgbot/internal/domain/billing"
)

const defaultBaseURL = "https://api.hetzner.cloud/v1"

var (
	ErrServerNotFound   = errors.New("hetzner server not found")
	ErrProviderAPIError = errors.New("hetzner api returned an error")
	ErrMissingToken     = errors.New("Hetzner Cloud API token is not configured (HCLOUD_TOKEN is empty). Please set HCLOUD_TOKEN in /etc/manova/shop-tgbot/shopbot.env")
)

type resourceCache struct {
	serverTypes   []port.ProviderServerType
	serverTypesAt time.Time

	locations   []port.ProviderLocation
	locationsAt time.Time

	images   []port.ProviderImage
	imagesAt time.Time
}

type simulatedServer struct {
	ID           string
	Name         string
	ServerType   string
	Image        string
	Location     string
	IPv4         string
	IPv6         string
	PowerState   billing.PowerState
	Status       string
	RootPassword string
	InRescue     bool
	CreatedAt    time.Time
}

// Client implements port.CloudProvider against Hetzner Cloud API,
// with 1-minute caching for catalog resources and strict production validation.
type Client struct {
	token          string
	baseURL        string
	httpClient     *http.Client
	logger         *slog.Logger
	simulationMode bool

	mu        sync.RWMutex
	cache     resourceCache
	simulated map[string]*simulatedServer
	idGen     atomic.Int64
}

// Config holds settings for Hetzner Cloud client.
type Config struct {
	Token          string
	BaseURL        string
	HTTPClient     *http.Client
	Logger         *slog.Logger
	SimulationMode bool // Only enabled in unit tests
}

// NewClient returns a new CloudProvider client.
func NewClient(cfg Config) *Client {
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 15 * time.Second}
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}

	c := &Client{
		token:          cfg.Token,
		baseURL:        baseURL,
		httpClient:     httpClient,
		logger:         logger,
		simulationMode: cfg.SimulationMode,
		simulated:      make(map[string]*simulatedServer),
	}
	c.idGen.Store(100000)
	return c
}

func (c *Client) isSimulated() bool {
	return c.simulationMode
}

// HasToken checks if API token is configured or running in simulation mode.
func (c *Client) HasToken() bool {
	return c.simulationMode || c.token != ""
}

// ListServerTypes returns available server types with a 1-minute cache.
func (c *Client) ListServerTypes(ctx context.Context) ([]port.ProviderServerType, error) {
	c.mu.RLock()
	if len(c.cache.serverTypes) > 0 && time.Since(c.cache.serverTypesAt) < 1*time.Minute {
		cached := append([]port.ProviderServerType(nil), c.cache.serverTypes...)
		c.mu.RUnlock()
		return cached, nil
	}
	c.mu.RUnlock()

	// If token present, fetch live from Hetzner API
	if c.token != "" {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/server_types", nil)
		if err == nil {
			req.Header.Set("Authorization", "Bearer "+c.token)
			resp, err := c.httpClient.Do(req)
			if err == nil {
				defer resp.Body.Close()
				if resp.StatusCode == http.StatusOK {
					var res struct {
						ServerTypes []struct {
							Name        string  `json:"name"`
							Description string  `json:"description"`
							Cores       int     `json:"cores"`
							Memory      float64 `json:"memory"`
							Disk        int     `json:"disk"`
						} `json:"server_types"`
					}
					if err := json.NewDecoder(resp.Body).Decode(&res); err == nil && len(res.ServerTypes) > 0 {
						var out []port.ProviderServerType
						for _, st := range res.ServerTypes {
							out = append(out, port.ProviderServerType{
								Name:        st.Name,
								Description: st.Description,
								Cores:       st.Cores,
								Memory:      int(st.Memory),
								Disk:        st.Disk,
							})
						}
						c.mu.Lock()
						c.cache.serverTypes = out
						c.cache.serverTypesAt = time.Now().UTC()
						c.mu.Unlock()
						return out, nil
					}
				}
			}
		}
	}

	// Default baseline fallback
	defaults := []port.ProviderServerType{
		{Name: "cx23", Description: "CX23", Cores: 2, Memory: 4, Disk: 40},
		{Name: "cx33", Description: "CX33", Cores: 4, Memory: 8, Disk: 80},
		{Name: "cx43", Description: "CX43", Cores: 8, Memory: 16, Disk: 160},
	}
	c.mu.Lock()
	c.cache.serverTypes = defaults
	c.cache.serverTypesAt = time.Now().UTC()
	c.mu.Unlock()
	return defaults, nil
}

// ListLocations returns available datacenter locations with a 1-minute cache.
func (c *Client) ListLocations(ctx context.Context) ([]port.ProviderLocation, error) {
	c.mu.RLock()
	if len(c.cache.locations) > 0 && time.Since(c.cache.locationsAt) < 1*time.Minute {
		cached := append([]port.ProviderLocation(nil), c.cache.locations...)
		c.mu.RUnlock()
		return cached, nil
	}
	c.mu.RUnlock()

	if c.token != "" {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/locations", nil)
		if err == nil {
			req.Header.Set("Authorization", "Bearer "+c.token)
			resp, err := c.httpClient.Do(req)
			if err == nil {
				defer resp.Body.Close()
				if resp.StatusCode == http.StatusOK {
					var res struct {
						Locations []struct {
							Name        string `json:"name"`
							Description string `json:"description"`
							City        string `json:"city"`
							Country     string `json:"country"`
						} `json:"locations"`
					}
					if err := json.NewDecoder(resp.Body).Decode(&res); err == nil && len(res.Locations) > 0 {
						var out []port.ProviderLocation
						for _, loc := range res.Locations {
							out = append(out, port.ProviderLocation{
								Name:        loc.Name,
								Description: loc.Description,
								City:        loc.City,
								Country:     loc.Country,
							})
						}
						c.mu.Lock()
						c.cache.locations = out
						c.cache.locationsAt = time.Now().UTC()
						c.mu.Unlock()
						return out, nil
					}
				}
			}
		}
	}

	defaults := []port.ProviderLocation{
		{Name: "nbg1", Description: "Nuremberg", City: "Nuremberg", Country: "DE"},
		{Name: "fsn1", Description: "Falkenstein", City: "Falkenstein", Country: "DE"},
		{Name: "hel1", Description: "Helsinki", City: "Helsinki", Country: "FI"},
	}
	c.mu.Lock()
	c.cache.locations = defaults
	c.cache.locationsAt = time.Now().UTC()
	c.mu.Unlock()
	return defaults, nil
}

// ListImages returns available system images with a 1-minute cache.
func (c *Client) ListImages(ctx context.Context) ([]port.ProviderImage, error) {
	c.mu.RLock()
	if len(c.cache.images) > 0 && time.Since(c.cache.imagesAt) < 1*time.Minute {
		cached := append([]port.ProviderImage(nil), c.cache.images...)
		c.mu.RUnlock()
		return cached, nil
	}
	c.mu.RUnlock()

	if c.token != "" {
		url := c.baseURL + "/images?type=system&architecture=x86"
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err == nil {
			req.Header.Set("Authorization", "Bearer "+c.token)
			resp, err := c.httpClient.Do(req)
			if err == nil {
				defer resp.Body.Close()
				if resp.StatusCode == http.StatusOK {
					var res struct {
						Images []struct {
							Name        string `json:"name"`
							Description string `json:"description"`
							OSFlavor    string `json:"os_flavor"`
							OSVersion   string `json:"os_version"`
							Status      string `json:"status"`
						} `json:"images"`
					}
					if err := json.NewDecoder(resp.Body).Decode(&res); err == nil && len(res.Images) > 0 {
						var out []port.ProviderImage
						for _, img := range res.Images {
							if img.Status == "available" && (img.OSFlavor == "ubuntu" || img.OSFlavor == "debian") {
								out = append(out, port.ProviderImage{
									Name:        img.Name,
									Description: img.Description,
									OSFlavor:    img.OSFlavor,
									OSVersion:   img.OSVersion,
								})
							}
						}
						if len(out) > 0 {
							c.mu.Lock()
							c.cache.images = out
							c.cache.imagesAt = time.Now().UTC()
							c.mu.Unlock()
							return out, nil
						}
					}
				}
			}
		}
	}

	defaults := []port.ProviderImage{
		{Name: "ubuntu-24.04", Description: "Ubuntu 24.04 LTS", OSFlavor: "ubuntu", OSVersion: "24.04"},
		{Name: "ubuntu-22.04", Description: "Ubuntu 22.04 LTS", OSFlavor: "ubuntu", OSVersion: "22.04"},
		{Name: "debian-12", Description: "Debian 12", OSFlavor: "debian", OSVersion: "12"},
		{Name: "debian-11", Description: "Debian 11", OSFlavor: "debian", OSVersion: "11"},
	}
	c.mu.Lock()
	c.cache.images = defaults
	c.cache.imagesAt = time.Now().UTC()
	c.mu.Unlock()
	return defaults, nil
}

// EnsureSSHKey ensures public key is registered in Hetzner Cloud and returns its key name or ID.
func (c *Client) EnsureSSHKey(ctx context.Context, name string, publicKey string) (string, error) {
	if c.isSimulated() {
		return name, nil
	}
	if c.token == "" {
		return "", ErrMissingToken
	}

	cleanKey := strings.TrimSpace(publicKey)
	payload := map[string]string{
		"name":       name,
		"public_key": cleanKey,
	}
	data, _ := json.Marshal(payload)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/ssh_keys", bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusCreated {
		var res struct {
			SSHKey struct {
				ID   int64  `json:"id"`
				Name string `json:"name"`
			} `json:"ssh_key"`
		}
		if err := json.Unmarshal(bodyBytes, &res); err == nil && res.SSHKey.Name != "" {
			return res.SSHKey.Name, nil
		}
		return name, nil
	}

	// If already registered with uniqueness error, find existing key
	var errResp struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	_ = json.Unmarshal(bodyBytes, &errResp)
	if errResp.Error.Code == "uniqueness_error" {
		listReq, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/ssh_keys", nil)
		if err == nil {
			listReq.Header.Set("Authorization", "Bearer "+c.token)
			if listResp, err := c.httpClient.Do(listReq); err == nil {
				defer listResp.Body.Close()
				var listRes struct {
					SSHKeys []struct {
						ID        int64  `json:"id"`
						Name      string `json:"name"`
						PublicKey string `json:"public_key"`
					} `json:"ssh_keys"`
				}
				if err := json.NewDecoder(listResp.Body).Decode(&listRes); err == nil {
					for _, k := range listRes.SSHKeys {
						if strings.TrimSpace(k.PublicKey) == cleanKey || k.Name == name {
							return k.Name, nil
						}
					}
				}
			}
		}
	}

	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("Hetzner SSH key error (%s): %s", errResp.Error.Code, errResp.Error.Message)
	}
	return name, nil
}

// CreateServer creates a server in Hetzner Cloud.
func (c *Client) CreateServer(ctx context.Context, name string, serverType string, imageName string, location string, sshKeyIDs []string) (*port.ProviderCreateResult, error) {
	if c.isSimulated() {
		return c.createSimulatedServer(name, serverType, imageName, location, len(sshKeyIDs) > 0)
	}

	if c.token == "" {
		return nil, ErrMissingToken
	}

	// Prepare SSH keys: if any key is raw OpenSSH key, ensure it is registered on Hetzner
	var hetznerSSHKeys []string
	for i, k := range sshKeyIDs {
		trimmed := strings.TrimSpace(k)
		if strings.HasPrefix(trimmed, "ssh-") || strings.HasPrefix(trimmed, "ecdsa-") {
			keyName := fmt.Sprintf("%s-key-%d", name, i+1)
			realKeyName, err := c.EnsureSSHKey(ctx, keyName, trimmed)
			if err != nil {
				return nil, fmt.Errorf("failed to register SSH key on Hetzner: %w", err)
			}
			hetznerSSHKeys = append(hetznerSSHKeys, realKeyName)
		} else {
			hetznerSSHKeys = append(hetznerSSHKeys, trimmed)
		}
	}

	reqBody := map[string]any{
		"name":               name,
		"server_type":        serverType,
		"image":              imageName,
		"location":           location,
		"start_after_create": true,
	}
	if len(hetznerSSHKeys) > 0 {
		reqBody["ssh_keys"] = hetznerSSHKeys
	}

	data, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/servers", bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Hetzner API: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var errResp struct {
			Error struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		_ = json.Unmarshal(bodyBytes, &errResp)
		if errResp.Error.Message != "" {
			return nil, fmt.Errorf("Hetzner API error (%s): %s", errResp.Error.Code, errResp.Error.Message)
		}
		return nil, fmt.Errorf("Hetzner API HTTP %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var res struct {
		Server struct {
			ID        int64  `json:"id"`
			Status    string `json:"status"`
			PublicNet struct {
				IPv4 struct {
					IP string `json:"ip"`
				} `json:"ipv4"`
				IPv6 struct {
					IP string `json:"ip"`
				} `json:"ipv6"`
			} `json:"public_net"`
		} `json:"server"`
		Action struct {
			ID int64 `json:"id"`
		} `json:"action"`
		RootPassword string `json:"root_password"`
	}

	if err := json.Unmarshal(bodyBytes, &res); err != nil {
		return nil, fmt.Errorf("failed to parse Hetzner response: %w", err)
	}

	ipv4 := res.Server.PublicNet.IPv4.IP
	ipv6 := res.Server.PublicNet.IPv6.IP
	extID := strconv.FormatInt(res.Server.ID, 10)

	// If IPv4 is not immediately populated, poll for up to 5 seconds
	if ipv4 == "" {
		for poll := 0; poll < 5; poll++ {
			time.Sleep(1 * time.Second)
			status, err := c.GetServer(ctx, extID)
			if err == nil && status.IPv4Address != "" {
				ipv4 = status.IPv4Address
				ipv6 = status.IPv6Address
				break
			}
		}
	}

	return &port.ProviderCreateResult{
		ExternalID:   extID,
		ActionID:     res.Action.ID,
		RootPassword: res.RootPassword,
		IPv4Address:  ipv4,
		IPv6Address:  ipv6,
		Status:       res.Server.Status,
	}, nil
}

// GetServer fetches server status.
func (c *Client) GetServer(ctx context.Context, externalID string) (*port.ProviderServerStatus, error) {
	if c.isSimulated() {
		return c.getSimulatedServer(externalID)
	}

	if c.token == "" {
		return nil, ErrMissingToken
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("%s/servers/%s", c.baseURL, externalID), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, ErrServerNotFound
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("%w: status %d", ErrProviderAPIError, resp.StatusCode)
	}

	var res struct {
		Server struct {
			ID        int64     `json:"id"`
			Status    string    `json:"status"`
			Created   time.Time `json:"created"`
			PublicNet struct {
				IPv4 struct {
					IP string `json:"ip"`
				} `json:"ipv4"`
				IPv6 struct {
					IP string `json:"ip"`
				} `json:"ipv6"`
			} `json:"public_net"`
		} `json:"server"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil, err
	}

	powerState := billing.PowerStateRunning
	if res.Server.Status == "off" {
		powerState = billing.PowerStateOff
	}

	return &port.ProviderServerStatus{
		ExternalID:  strconv.FormatInt(res.Server.ID, 10),
		Status:      res.Server.Status,
		PowerState:  powerState,
		Created:     res.Server.Created,
		IPv4Address: res.Server.PublicNet.IPv4.IP,
		IPv6Address: res.Server.PublicNet.IPv6.IP,
	}, nil
}

// DeleteServer deletes server.
func (c *Client) DeleteServer(ctx context.Context, externalID string) error {
	if c.isSimulated() {
		c.mu.Lock()
		defer c.mu.Unlock()
		delete(c.simulated, externalID)
		return nil
	}

	if c.token == "" {
		return ErrMissingToken
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, fmt.Sprintf("%s/servers/%s", c.baseURL, externalID), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 && resp.StatusCode != http.StatusNotFound {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("Hetzner delete server error (%d): %s", resp.StatusCode, string(bodyBytes))
	}
	return nil
}

// PowerOn powers on a server.
func (c *Client) PowerOn(ctx context.Context, externalID string) error {
	if c.isSimulated() {
		c.mu.Lock()
		defer c.mu.Unlock()
		if s, ok := c.simulated[externalID]; ok {
			s.PowerState = billing.PowerStateRunning
			s.Status = "running"
		}
		return nil
	}
	return c.postAction(ctx, externalID, "poweron", nil)
}

// PowerOff powers off a server.
func (c *Client) PowerOff(ctx context.Context, externalID string) error {
	if c.isSimulated() {
		c.mu.Lock()
		defer c.mu.Unlock()
		if s, ok := c.simulated[externalID]; ok {
			s.PowerState = billing.PowerStateOff
			s.Status = "off"
		}
		return nil
	}
	return c.postAction(ctx, externalID, "poweroff", nil)
}

// Shutdown gracefully shuts down server.
func (c *Client) Shutdown(ctx context.Context, externalID string) error {
	if c.isSimulated() {
		return c.PowerOff(ctx, externalID)
	}
	return c.postAction(ctx, externalID, "shutdown", nil)
}

// Reboot restarts a server.
func (c *Client) Reboot(ctx context.Context, externalID string) error {
	if c.isSimulated() {
		c.mu.Lock()
		defer c.mu.Unlock()
		if s, ok := c.simulated[externalID]; ok {
			s.PowerState = billing.PowerStateRunning
			s.Status = "running"
		}
		return nil
	}
	return c.postAction(ctx, externalID, "reboot", nil)
}

// ResetPassword resets the root password of server.
func (c *Client) ResetPassword(ctx context.Context, externalID string) (string, error) {
	if c.isSimulated() {
		c.mu.Lock()
		defer c.mu.Unlock()
		pwd := generateRandomPassword(18)
		if s, ok := c.simulated[externalID]; ok {
			s.RootPassword = pwd
		}
		return pwd, nil
	}

	body, err := c.postActionWithResponse(ctx, externalID, "reset_password", nil)
	if err != nil {
		return "", err
	}
	var res struct {
		RootPassword string `json:"root_password"`
	}
	_ = json.Unmarshal(body, &res)
	if res.RootPassword == "" {
		return "", errors.New("empty password returned by Hetzner reset_password")
	}
	return res.RootPassword, nil
}

// EnableRescue enables the Hetzner rescue system.
func (c *Client) EnableRescue(ctx context.Context, externalID string) (string, error) {
	if c.isSimulated() {
		c.mu.Lock()
		defer c.mu.Unlock()
		rescuePwd := generateRandomPassword(18)
		if s, ok := c.simulated[externalID]; ok {
			s.InRescue = true
		}
		return rescuePwd, nil
	}

	payload := map[string]string{"type": "linux64"}
	body, err := c.postActionWithResponse(ctx, externalID, "enable_rescue", payload)
	if err != nil {
		return "", err
	}
	var res struct {
		RootPassword string `json:"root_password"`
	}
	_ = json.Unmarshal(body, &res)
	if res.RootPassword == "" {
		return "", errors.New("empty password returned by Hetzner enable_rescue")
	}
	return res.RootPassword, nil
}

// DisableRescue disables rescue mode.
func (c *Client) DisableRescue(ctx context.Context, externalID string) error {
	if c.isSimulated() {
		c.mu.Lock()
		defer c.mu.Unlock()
		if s, ok := c.simulated[externalID]; ok {
			s.InRescue = false
		}
		return nil
	}
	return c.postAction(ctx, externalID, "disable_rescue", nil)
}

func (c *Client) postAction(ctx context.Context, externalID, action string, payload any) error {
	_, err := c.postActionWithResponse(ctx, externalID, action, payload)
	return err
}

func (c *Client) postActionWithResponse(ctx context.Context, externalID, action string, payload any) ([]byte, error) {
	if c.token == "" {
		return nil, ErrMissingToken
	}

	url := fmt.Sprintf("%s/servers/%s/actions/%s", c.baseURL, externalID, action)
	var bodyReader io.Reader
	if payload != nil {
		d, _ := json.Marshal(payload)
		bodyReader = bytes.NewReader(d)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bodyReader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var errResp struct {
			Error struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		_ = json.Unmarshal(data, &errResp)
		if errResp.Error.Message != "" {
			return nil, fmt.Errorf("Hetzner action error (%s): %s", errResp.Error.Code, errResp.Error.Message)
		}
		return nil, fmt.Errorf("%w: status %d", ErrProviderAPIError, resp.StatusCode)
	}
	return data, nil
}

// Simulated helpers (used ONLY when SimulationMode: true in unit tests)
func (c *Client) createSimulatedServer(name, serverType, imageName, location string, hasSSHKey bool) (*port.ProviderCreateResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	idNum := c.idGen.Add(1)
	extID := strconv.FormatInt(idNum, 10)

	var ipv4Prefix string
	switch location {
	case "fsn1":
		ipv4Prefix = "168.119."
	case "hel1":
		ipv4Prefix = "65.108."
	default:
		ipv4Prefix = "159.69."
	}
	ipv4 := fmt.Sprintf("%s%d.%d", ipv4Prefix, (idNum/256)%250+1, idNum%250+1)
	ipv6 := fmt.Sprintf("2a01:4f8:1c1c:%x::1", idNum)

	var rootPwd string
	if !hasSSHKey {
		rootPwd = generateRandomPassword(20)
	}

	s := &simulatedServer{
		ID:           extID,
		Name:         name,
		ServerType:   serverType,
		Image:        imageName,
		Location:     location,
		IPv4:         ipv4,
		IPv6:         ipv6,
		PowerState:   billing.PowerStateRunning,
		Status:       "running",
		RootPassword: rootPwd,
		CreatedAt:    time.Now().UTC(),
	}
	c.simulated[extID] = s

	return &port.ProviderCreateResult{
		ExternalID:   extID,
		ActionID:     idNum * 10,
		RootPassword: rootPwd,
		IPv4Address:  ipv4,
		IPv6Address:  ipv6,
		Status:       "running",
	}, nil
}

func (c *Client) getSimulatedServer(externalID string) (*port.ProviderServerStatus, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	s, ok := c.simulated[externalID]
	if !ok {
		return &port.ProviderServerStatus{
			ExternalID:  externalID,
			Status:      "running",
			PowerState:  billing.PowerStateRunning,
			Created:     time.Now().UTC().Add(-time.Hour),
			IPv4Address: "159.69.100.1",
			IPv6Address: "2a01:4f8:1c1c:1000::1",
		}, nil
	}

	return &port.ProviderServerStatus{
		ExternalID:  s.ID,
		Status:      s.Status,
		PowerState:  s.PowerState,
		Created:     s.CreatedAt,
		IPv4Address: s.IPv4,
		IPv6Address: s.IPv6,
	}, nil
}

// GenerateRandomPassword creates a strong random password for root access.
func GenerateRandomPassword(length int) string {
	return generateRandomPassword(length)
}

func generateRandomPassword(length int) string {
	if length < 12 {
		length = 16
	}
	const chars = "abcdefghjkmnpqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ23456789!#%*+=-"
	result := make([]byte, length)
	for i := range result {
		num, err := rand.Int(rand.Reader, big.NewInt(int64(len(chars))))
		if err != nil {
			result[i] = chars[i%len(chars)]
		} else {
			result[i] = chars[num.Int64()]
		}
	}
	return string(result)
}

var _ port.CloudProvider = (*Client)(nil)
