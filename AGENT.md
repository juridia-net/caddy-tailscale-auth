# Caddy Tailscale Auth Plugin - Agent Guidelines

## Build/Test Commands
- **Build**: `earthly +binary` (creates ./caddy with both plugins)
- **Manual Build**: `xcaddy build latest --with github.com/caddy-dns/cloudflare --with github.com/mholt/caddy-l4 --with caddyauth=.`
- **Test Run**: `./caddy run --config Caddyfile`
- **Go Commands**: `go mod tidy`, `go fmt`, `go vet`
- **Test**: No test files found - create `*_test.go` files following Go conventions

## Architecture
- **Single Module**: `github.com/juridia-net/caddy-tailscale-auth` - Caddy v2 HTTP middleware plugin
- **Main File**: `tailscale.go` - Contains `TailscaleAuth` struct implementing Caddy interfaces
- **External APIs**: Tailscale API (`api.tailscale.com`) for device lookup via API key
- **Cache System**: JSON file-based device cache with in-memory map for performance
- **No Database**: Uses local JSON cache file for persistence

## Code Style
- **Package**: `caddyauth` (single Go package)
- **Imports**: Standard library first, then external, then Caddy modules
- **Naming**: CamelCase for exported, camelCase for unexported
- **Structs**: JSON tags for API/config fields, clear field documentation
- **Error Handling**: `fmt.Errorf` with wrapped errors, structured logging with zap
- **Logging**: Use `t.logger` (zap) with structured fields (`zap.String`, `zap.Error`)
- **No Comments**: Code is self-documenting, avoid unnecessary comments
