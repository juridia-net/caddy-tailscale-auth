package caddyauth

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	"github.com/caddyserver/caddy/v2/caddyconfig/httpcaddyfile"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	"go.uber.org/zap"
)

func init() {
	caddy.RegisterModule(TailscaleAuth{})
	httpcaddyfile.RegisterHandlerDirective("tailscale_auth", parseCaddyfile)
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

	logger *zap.Logger
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
func (TailscaleAuth) CaddyModule() caddy.ModuleInfo {
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

	if t.Tailnet == "" {
		return fmt.Errorf("tailnet is required")
	}

	if t.APIKey == "" {
		return fmt.Errorf("api_key is required")
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

	// Get Tailscale user information
	whois, err := t.getTailscaleUser(clientIP)
	if err != nil {
		t.logger.Error("failed to get Tailscale user info",
			zap.String("client_ip", clientIP),
			zap.Error(err))
		// Continue with the request even if Tailscale lookup fails
		return next.ServeHTTP(w, r)
	}

	// Add user information to headers
	t.addUserHeaders(r, whois)

	return next.ServeHTTP(w, r)
}

// getTailscaleUser fetches user information from Tailscale API
func (t *TailscaleAuth) getTailscaleUser(clientIP string) (*WhoIsResponse, error) {
	url := fmt.Sprintf("https://api.tailscale.com/api/v2/tailnet/%s/whois?addr=%s", t.Tailnet, clientIP)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+t.APIKey)
	req.Header.Set("User-Agent", "Caddy-Tailscale-Auth/1.0")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API request failed with status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	var whois WhoIsResponse
	if err := json.Unmarshal(body, &whois); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	return &whois, nil
}

// addUserHeaders adds Tailscale user information to request headers
func (t *TailscaleAuth) addUserHeaders(r *http.Request, whois *WhoIsResponse) {
	// Node information
	r.Header.Set(t.HeaderPrefix+"Node-ID", whois.Node.ID)
	r.Header.Set(t.HeaderPrefix+"Node-Name", whois.Node.Name)
	r.Header.Set(t.HeaderPrefix+"Node-User", whois.Node.User)
	r.Header.Set(t.HeaderPrefix+"Node-Hostname", whois.Node.Hostname)
	r.Header.Set(t.HeaderPrefix+"Node-OS", whois.Node.OS)
	r.Header.Set(t.HeaderPrefix+"Node-Online", fmt.Sprintf("%t", whois.Node.Online))
	r.Header.Set(t.HeaderPrefix+"Node-Expired", fmt.Sprintf("%t", whois.Node.Expired))

	// User profile information
	r.Header.Set(t.HeaderPrefix+"User-ID", whois.UserProfile.ID)
	r.Header.Set(t.HeaderPrefix+"User-LoginName", whois.UserProfile.LoginName)
	r.Header.Set(t.HeaderPrefix+"User-DisplayName", whois.UserProfile.DisplayName)

	// Node addresses (join multiple addresses with comma)
	if len(whois.Node.Addresses) > 0 {
		r.Header.Set(t.HeaderPrefix+"Node-Addresses", strings.Join(whois.Node.Addresses, ","))
	}

	// Node tags (join multiple tags with comma)
	if len(whois.Node.Tags) > 0 {
		r.Header.Set(t.HeaderPrefix+"Node-Tags", strings.Join(whois.Node.Tags, ","))
	}
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
			}
		}
	}
	return nil
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
		return strings.TrimSpace(xri)
	}

	// Fall back to RemoteAddr
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
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
