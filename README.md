# Caddy Tailscale Auth Plugin

A Caddy plugin that integrates with Tailscale's API to authenticate users and inject user information into HTTP request headers. This plugin fetches user details from Tailscale's WhoIs API and adds them as headers for downstream applications to use.

## Features

- 🔐 **Automatic User Authentication**: Queries Tailscale API to get user information based on client IP
- 📝 **Header Injection**: Adds comprehensive user and node information to request headers
- ⚡ **Non-blocking**: Continues processing requests even if Tailscale lookup fails
- 🛠️ **Configurable**: Customizable API endpoints, header prefixes, and more
- 🚀 **High Performance**: Efficient HTTP client with proper error handling

## Installation

### Using Earthly (Recommended)

This project includes an Earthfile that builds a custom Caddy binary with both the Cloudflare DNS plugin and this Tailscale auth plugin.

```powershell
# Build the custom Caddy binary
earthly +binary

# The compiled binary will be saved as ./caddy
```

### Manual Build with xcaddy

```bash
xcaddy build latest \
    --with github.com/caddy-dns/cloudflare \
    --with github.com/juridia-net/caddy-tailscale-auth
```

## Configuration

### Caddyfile Syntax

```caddyfile
example.com {
    tailscale_auth {
        api_key "tskey-api-your-api-key-here"
        tailnet "your-tailnet.net"
        header_prefix "X-Tailscale-"
    }
    
    # Your other directives
    reverse_proxy localhost:8080
}
```

### Configuration Options

| Option | Required | Default | Description |
|--------|----------|---------|-------------|
| `api_key` | Yes | - | Your Tailscale API key (tskey-xxx) |
| `tailnet` | Yes | - | Your Tailnet domain (e.g., "juridia.net") |
| `header_prefix` | No | "X-Tailscale-" | Prefix for injected headers |

### JSON Configuration

```json
{
  "handler": "tailscale_auth",
  "api_key": "tskey-api-your-api-key-here",
  "tailnet": "your-tailnet.net",
  "header_prefix": "X-Tailscale-"
}
```

## Generated Headers

The plugin injects the following headers into requests:

### Node Information
- `X-Tailscale-Node-ID`: Unique node identifier
- `X-Tailscale-Node-Name`: Node name in Tailscale
- `X-Tailscale-Node-User`: User ID associated with the node
- `X-Tailscale-Node-Hostname`: Device hostname
- `X-Tailscale-Node-OS`: Operating system
- `X-Tailscale-Node-Online`: Whether the node is currently online (true/false)
- `X-Tailscale-Node-Expired`: Whether the node key has expired (true/false)
- `X-Tailscale-Node-Addresses`: Comma-separated list of IP addresses
- `X-Tailscale-Node-Tags`: Comma-separated list of ACL tags

### User Profile Information
- `X-Tailscale-User-ID`: Unique user identifier
- `X-Tailscale-User-LoginName`: User's login name (email)
- `X-Tailscale-User-DisplayName`: User's display name

## API Requirements

### Tailscale API Key

You need a Tailscale API key with appropriate permissions:

1. Go to the [Tailscale Admin Console](https://login.tailscale.com/admin/settings/keys)
2. Generate an API key with these scopes:
   - `devices:read` - To read device information
   - `users:read` - To read user profile information

### Network Requirements

The plugin needs outbound HTTPS access to:
- `api.tailscale.com` (port 443)

## Example Usage

### Basic Authentication Proxy

```caddyfile
app.example.com {
    tailscale_auth {
        api_key {env.TAILSCALE_API_KEY}
        tailnet "mycompany.net"
    }
    
    reverse_proxy localhost:3000 {
        # Pass through all Tailscale headers
        header_up X-Tailscale-* {>X-Tailscale-*}
    }
}
```

### With Custom Header Prefix

```caddyfile
api.example.com {
    tailscale_auth {
        api_key {env.TAILSCALE_API_KEY}
        tailnet "mycompany.net"
        header_prefix "TS-Auth-"
    }
    
    reverse_proxy localhost:8080
}
```

### Environment Variables

Store sensitive configuration in environment variables:

```bash
export TAILSCALE_API_KEY="tskey-api-your-key-here"
```

```caddyfile
example.com {
    tailscale_auth {
        api_key {env.TAILSCALE_API_KEY}
        tailnet "mycompany.net"
    }
    
    reverse_proxy localhost:8080
}
```

## Development

### Prerequisites

- Go 1.24+
- Earthly (for containerized builds)
- Git

### Local Development

```bash
# Clone the repository
git clone https://github.com/juridia-net/caddy-tailscale-auth.git
cd caddy-tailscale-auth

# Install dependencies
go mod tidy

# Build with xcaddy for testing
xcaddy build --with github.com/juridia-net/caddy-tailscale-auth=.

# Test the plugin
./caddy run --config Caddyfile
```

### Testing

Create a test Caddyfile:

```caddyfile
localhost:8080 {
    tailscale_auth {
        api_key "your-test-key"
        tailnet "your-tailnet.net"
    }
    
    respond "Headers: {http.request.header}" 200
}
```

## Troubleshooting

### Common Issues

1. **API Key Invalid**
   ```
   failed to get Tailscale user info: API request failed with status 401
   ```
   - Verify your API key is correct and has proper permissions
   - Check that the key hasn't expired

2. **Tailnet Not Found**
   ```
   failed to get Tailscale user info: API request failed with status 404
   ```
   - Verify the tailnet name is correct (without https://)
   - Ensure the API key has access to the specified tailnet

3. **Client IP Not Found**
   ```
   could not determine client IP
   ```
   - The plugin couldn't extract a valid IP from the request
   - Check your reverse proxy configuration and X-Forwarded-For headers

### Debug Mode

Enable debug logging in Caddy:

```json
{
  "logging": {
    "logs": {
      "default": {
        "level": "DEBUG"
      }
    }
  }
}
```

### Network Connectivity

Test API connectivity:

```bash
curl -H "Authorization: Bearer tskey-your-key" \
     "https://api.tailscale.com/api/v2/tailnet/your-tailnet.net/whois?addr=100.64.0.1"
```

## Security Considerations

- 🔒 **Store API keys securely**: Use environment variables or secure config management
- 🛡️ **Limit API key scope**: Only grant necessary permissions (devices:read, users:read)
- 🔄 **Rotate keys regularly**: Follow your organization's key rotation policy
- 📝 **Monitor usage**: Track API calls in Tailscale admin console
- 🚫 **Don't log sensitive headers**: Be careful with access logs containing user information

## Contributing

1. Fork the repository
2. Create a feature branch (`git checkout -b feature/amazing-feature`)
3. Commit your changes (`git commit -m 'Add some amazing feature'`)
4. Push to the branch (`git push origin feature/amazing-feature`)
5. Open a Pull Request

### Code Style

- Follow standard Go formatting (`go fmt`)
- Add tests for new functionality
- Update documentation for API changes
- Use conventional commit messages

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

## Changelog

### v1.0.0
- Initial release
- Basic Tailscale API integration
- Header injection functionality
- Caddyfile configuration support
- Error handling and logging

## Related Projects

- [Caddy](https://caddyserver.com/) - The web server this plugin extends
- [Tailscale](https://tailscale.com/) - The VPN service this plugin integrates with
- [xcaddy](https://github.com/caddyserver/xcaddy) - Tool for building Caddy with plugins

## Support

- 📖 [Documentation](https://github.com/juridia-net/caddy-tailscale-auth/wiki)
- 🐛 [Issue Tracker](https://github.com/juridia-net/caddy-tailscale-auth/issues)
- 💬 [Discussions](https://github.com/juridia-net/caddy-tailscale-auth/discussions)
- 📧 [Email Support](mailto:support@juridia.net)

---

Made with ❤️ by [Juridia](https://juridia.net)
