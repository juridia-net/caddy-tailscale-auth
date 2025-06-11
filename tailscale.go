package caddyauth

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	"github.com/caddyserver/caddy/v2/caddyconfig/httpcaddyfile"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	"go.uber.org/zap"
)

func init() {
	caddy.RegisterModule((*TailscaleAuth)(nil))
	httpcaddyfile.RegisterHandlerDirective("tailscale_auth", parseCaddyfile)
}

// Device represents a Tailscale device from the API
type Device struct {
	Addresses                 []string `json:"addresses"`
	Authorized                bool     `json:"authorized"`
	BlocksIncomingConnections bool     `json:"blocksIncomingConnections"`
	ClientVersion             string   `json:"clientVersion"`
	Created                   string   `json:"created"`
	Expires                   string   `json:"expires"`
	Hostname                  string   `json:"hostname"`
	ID                        string   `json:"id"`
	IsExternal                bool     `json:"isExternal"`
	KeyExpiryDisabled         bool     `json:"keyExpiryDisabled"`
	LastSeen                  string   `json:"lastSeen"`
	MachineKey                string   `json:"machineKey"`
	Name                      string   `json:"name"`
	NodeID                    string   `json:"nodeId"`
	NodeKey                   string   `json:"nodeKey"`
	OS                        string   `json:"os"`
	TailnetLockError          string   `json:"tailnetLockError"`
	TailnetLockKey            string   `json:"tailnetLockKey"`
	UpdateAvailable           bool     `json:"updateAvailable"`
	User                      string   `json:"user"`
}

// DevicesResponse represents the response from Tailscale's devices API
type DevicesResponse struct {
	Devices []Device `json:"devices"`
}

// DeviceCache represents the cached device information
type DeviceCache struct {
	IPToDevice map[string]*Device `json:"ip_to_device"`
	LastUpdate string             `json:"last_update"`
}

// TailscaleAuth is a Caddy module that fetches Tailscale user information
// and adds it to request headers.
type TailscaleAuth struct {
	// APIKey is the Tailscale API key for authentication
	APIKey string `json:"api_key,omitempty"`

	// Tailnet is the Tailscale tailnet name (e.g., "juridia.net")
	Tailnet string `json:"tailnet,omitempty"`

	// HeaderPrefix is the prefix for headers that will be added (default: "X-Tailscale-")
	HeaderPrefix string `json:"header_prefix,omitempty"`

	// CacheFile is the path to store the device cache (default: "tailscale_devices.json")
	CacheFile string `json:"cache_file,omitempty"`

	logger      *zap.Logger
	deviceCache *DeviceCache
	cacheMutex  sync.RWMutex
}

// WhoIsResponse represents the response from Tailscale's whois API
type WhoIsResponse struct {
	Node struct {
		ID            string   `json:"id"`
		Name          string   `json:"name"`
		User          string   `json:"user"`
		Tailnet       string   `json:"tailnet"`
		Hostname      string   `json:"hostname"`
		ClientVersion string   `json:"clientVersion"`
		OS            string   `json:"os"`
		Created       string   `json:"created"`
		LastSeen      string   `json:"lastSeen"`
		Online        bool     `json:"online"`
		Expired       bool     `json:"expired"`
		KeyExpiry     string   `json:"keyExpiry"`
		MachineKey    string   `json:"machineKey"`
		NodeKey       string   `json:"nodeKey"`
		Addresses     []string `json:"addresses"`
		Tags          []string `json:"tags"`
	} `json:"Node"`
	UserProfile struct {
		ID            string `json:"id"`
		LoginName     string `json:"loginName"`
		DisplayName   string `json:"displayName"`
		ProfilePicURL string `json:"profilePicURL"`
	} `json:"UserProfile"`
	CapMap map[string][]string `json:"CapMap"`
}

// CaddyModule returns the Caddy module information.
func (*TailscaleAuth) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{
		ID:  "http.handlers.tailscale_auth",
		New: func() caddy.Module { return new(TailscaleAuth) },
	}
}

// Provision implements caddy.Provisioner.
func (t *TailscaleAuth) Provision(ctx caddy.Context) error {
	t.logger = ctx.Logger(t)

	// Set default values
	if t.HeaderPrefix == "" {
		t.HeaderPrefix = "X-Tailscale-"
	}

	if t.CacheFile == "" {
		t.CacheFile = "tailscale_devices.json"
	}

	if t.Tailnet == "" {
		return fmt.Errorf("tailnet is required")
	}

	if t.APIKey == "" {
		return fmt.Errorf("api_key is required")
	}

	// Initialize device cache
	t.deviceCache = &DeviceCache{
		IPToDevice: make(map[string]*Device),
	}

	// Load existing cache from disk
	if err := t.loadDeviceCache(); err != nil {
		t.logger.Warn("failed to load device cache, starting with empty cache", zap.Error(err))
	}

	return nil
}

// Validate implements caddy.Validator.
func (t *TailscaleAuth) Validate() error {
	if t.Tailnet == "" {
		return fmt.Errorf("tailnet is required")
	}

	if t.APIKey == "" {
		return fmt.Errorf("api_key is required")
	}

	return nil
}

// ServeHTTP implements caddyhttp.MiddlewareHandler.
func (t *TailscaleAuth) ServeHTTP(w http.ResponseWriter, r *http.Request, next caddyhttp.Handler) error {
	// Get client IP
	clientIP := getClientIP(r)
	if clientIP == "" {
		t.logger.Warn("could not determine client IP")
		return next.ServeHTTP(w, r)
	}

	// Get device information from cache (will refresh if not found)
	device, err := t.getDeviceByIP(clientIP)
	if err != nil {
		t.logger.Error("failed to get device info",
			zap.String("client_ip", clientIP),
			zap.Error(err))
		// Continue with the request even if device lookup fails
		return next.ServeHTTP(w, r)
	}

	// Add device information to headers
	t.addDeviceHeaders(r, device)

	return next.ServeHTTP(w, r)
}

// addDeviceHeaders adds Tailscale device information to request headers
func (t *TailscaleAuth) addDeviceHeaders(r *http.Request, device *Device) {
	// Device information
	r.Header.Set(t.HeaderPrefix+"Device-ID", device.ID)
	r.Header.Set(t.HeaderPrefix+"Device-Name", device.Name)
	r.Header.Set(t.HeaderPrefix+"Device-User", device.User)
	r.Header.Set(t.HeaderPrefix+"Device-Hostname", device.Hostname)
	r.Header.Set(t.HeaderPrefix+"Device-OS", device.OS)
	r.Header.Set(t.HeaderPrefix+"Device-Authorized", fmt.Sprintf("%t", device.Authorized))
	r.Header.Set(t.HeaderPrefix+"Device-NodeID", device.NodeID)

	// Device addresses (join multiple addresses with comma)
	if len(device.Addresses) > 0 {
		r.Header.Set(t.HeaderPrefix+"Device-Addresses", strings.Join(device.Addresses, ","))
	}

	// Additional device metadata
	r.Header.Set(t.HeaderPrefix+"Device-ClientVersion", device.ClientVersion)
	r.Header.Set(t.HeaderPrefix+"Device-LastSeen", device.LastSeen)
	r.Header.Set(t.HeaderPrefix+"Device-Created", device.Created)
}

func (m *TailscaleAuth) UnmarshalCaddyfile(d *caddyfile.Dispenser) error {
	for d.Next() {
		for d.NextBlock(0) {
			switch d.Val() {
			case "api_key":
				if !d.NextArg() {
					return d.ArgErr()
				}
				m.APIKey = d.Val()

			case "tailnet":
				if !d.NextArg() {
					return d.ArgErr()
				}
				m.Tailnet = d.Val()

			case "header_prefix":
				if !d.NextArg() {
					m.HeaderPrefix = "X-Tailscale-"
				} else {
					m.HeaderPrefix = d.Val()
				}

			case "cache_file":
				if !d.NextArg() {
					return d.ArgErr()
				}
				m.CacheFile = d.Val()
			}
		}
	}
	return nil
}

// loadDeviceCache loads the device cache from disk
func (t *TailscaleAuth) loadDeviceCache() error {
	cacheFile := t.getCacheFilePath()

	data, err := os.ReadFile(cacheFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // Cache file doesn't exist, start with empty cache
		}
		return fmt.Errorf("failed to read cache file: %w", err)
	}

	t.cacheMutex.Lock()
	defer t.cacheMutex.Unlock()

	if err := json.Unmarshal(data, t.deviceCache); err != nil {
		return fmt.Errorf("failed to unmarshal cache: %w", err)
	}

	t.logger.Info("loaded device cache",
		zap.Int("device_count", len(t.deviceCache.IPToDevice)),
		zap.String("last_update", t.deviceCache.LastUpdate))

	return nil
}

// saveDeviceCache saves the device cache to disk
func (t *TailscaleAuth) saveDeviceCache() error {
	cacheFile := t.getCacheFilePath()

	// Create directory if it doesn't exist
	if err := os.MkdirAll(filepath.Dir(cacheFile), 0755); err != nil {
		return fmt.Errorf("failed to create cache directory: %w", err)
	}

	t.cacheMutex.RLock()
	data, err := json.MarshalIndent(t.deviceCache, "", "  ")
	t.cacheMutex.RUnlock()

	if err != nil {
		return fmt.Errorf("failed to marshal cache: %w", err)
	}

	if err := os.WriteFile(cacheFile, data, 0644); err != nil {
		return fmt.Errorf("failed to write cache file: %w", err)
	}

	return nil
}

// getCacheFilePath returns the full path to the cache file
func (t *TailscaleAuth) getCacheFilePath() string {
	if filepath.IsAbs(t.CacheFile) {
		return t.CacheFile
	}
	// If relative path, use current working directory
	return t.CacheFile
}

// refreshDeviceCache fetches the latest device list from Tailscale API
func (t *TailscaleAuth) refreshDeviceCache() error {
	url := fmt.Sprintf("https://api.tailscale.com/api/v2/tailnet/%s/devices", t.Tailnet)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+t.APIKey)
	req.Header.Set("User-Agent", "Caddy-Tailscale-Auth/1.0")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("API request failed with status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response body: %w", err)
	}

	var devicesResp DevicesResponse
	if err := json.Unmarshal(body, &devicesResp); err != nil {
		return fmt.Errorf("failed to unmarshal response: %w", err)
	}

	// Update cache with new device data
	t.cacheMutex.Lock()
	defer t.cacheMutex.Unlock()

	// Clear existing cache
	t.deviceCache.IPToDevice = make(map[string]*Device)

	// Populate cache with new devices
	for i := range devicesResp.Devices {
		device := &devicesResp.Devices[i]
		for _, addr := range device.Addresses {
			t.deviceCache.IPToDevice[addr] = device
		}
	}

	t.deviceCache.LastUpdate = resp.Header.Get("Date")

	t.logger.Info("refreshed device cache",
		zap.Int("device_count", len(devicesResp.Devices)),
		zap.Int("ip_mappings", len(t.deviceCache.IPToDevice)))

	// Save updated cache to disk
	if err := t.saveDeviceCache(); err != nil {
		t.logger.Error("failed to save device cache", zap.Error(err))
	}

	return nil
}

// getDeviceByIP returns the device for the given IP address, refreshing cache if needed
func (t *TailscaleAuth) getDeviceByIP(clientIP string) (*Device, error) {
	// First, check if device exists in cache
	t.cacheMutex.RLock()
	device, exists := t.deviceCache.IPToDevice[clientIP]
	t.cacheMutex.RUnlock()

	if exists && device != nil {
		return device, nil
	}

	// Device not found in cache, refresh and try again
	t.logger.Info("unknown device IP, refreshing cache", zap.String("client_ip", clientIP))

	if err := t.refreshDeviceCache(); err != nil {
		return nil, fmt.Errorf("failed to refresh device cache: %w", err)
	}

	// Check cache again after refresh
	t.cacheMutex.RLock()
	device, exists = t.deviceCache.IPToDevice[clientIP]
	t.cacheMutex.RUnlock()

	if !exists || device == nil {
		return nil, fmt.Errorf("device not found for IP %s even after cache refresh", clientIP)
	}

	return device, nil
}

// getClientIP extracts the client IP from the request
func getClientIP(r *http.Request) string {
	// Check X-Forwarded-For header first
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// Take the first IP in the list
		if idx := strings.Index(xff, ","); idx != -1 {
			return strings.TrimSpace(xff[:idx])
		}
		return strings.TrimSpace(xff)
	}

	// Check X-Real-IP header
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return xri
	}

	// Fall back to RemoteAddr
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}

	return r.RemoteAddr
}

// parseCaddyfile unmarshals tokens from h into a new Middleware.
func parseCaddyfile(h httpcaddyfile.Helper) (caddyhttp.MiddlewareHandler, error) {
	var t TailscaleAuth

	for h.Next() {
		for h.NextBlock(0) {
			switch h.Val() {
			case "api_key":
				if !h.NextArg() {
					return nil, h.ArgErr()
				}
				t.APIKey = h.Val()

			case "tailnet":
				if !h.NextArg() {
					return nil, h.ArgErr()
				}
				t.Tailnet = h.Val()

			case "header_prefix":
				if !h.NextArg() {
					return nil, h.ArgErr()
				}
				t.HeaderPrefix = h.Val()

			case "cache_file":
				if !h.NextArg() {
					return nil, h.ArgErr()
				}
				t.CacheFile = h.Val()

			default:
				return nil, h.Errf("unrecognized subdirective: %s", h.Val())
			}
		}
	}

	return &t, nil
}

// Interface guards
var (
	_ caddy.Provisioner           = (*TailscaleAuth)(nil)
	_ caddy.Validator             = (*TailscaleAuth)(nil)
	_ caddyhttp.MiddlewareHandler = (*TailscaleAuth)(nil)
	_ caddyfile.Unmarshaler       = (*TailscaleAuth)(nil)
)
