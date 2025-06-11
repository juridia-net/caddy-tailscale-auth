# Earthfile to build the latest Caddy binary with the latest Cloudflare DNS plugin.
#
# This version dynamically finds and downloads the latest pre-compiled xcaddy
# binary from GitHub Releases instead of using 'go install'.
#
# The final output is a single Caddy binary saved to your local filesystem.
VERSION 0.8

# --- Builder Stage ---
# This target sets up the build environment by downloading and installing
# the latest pre-compiled xcaddy .deb package.
builder:
    # We need a Debian-based image to install the .deb package.
    # We still need Go because xcaddy uses it to compile Caddy plugins.
    FROM golang:1.24-bookworm

    # Install dependencies:
    # - git:  Required by xcaddy to fetch Go modules for plugins.
    # - curl: Required to query the GitHub API and download the binary.
    # - jq:   A lightweight tool to parse the JSON response from the API.
    RUN apt-get update && apt-get install -y --no-install-recommends git curl jq

    # This is the core logic:
    # 1. Query the GitHub API for the latest xcaddy release.
    # 2. Use 'jq' to find the download URL for the 'linux_amd64.deb' asset.
    # 3. Download the .deb package using the URL found.
    # 4. Install the package with 'dpkg'.
    # 5. Clean up the downloaded file and apt cache.
    WORKDIR /tmp
    RUN LATEST_XCADDY_URL=$(curl -sSL "https://api.github.com/repos/caddyserver/xcaddy/releases/latest" | jq -r '.assets[] | select(.name | contains("linux_amd64.deb")) | .browser_download_url') && \
        echo "Downloading xcaddy from ${LATEST_XCADDY_URL}" && \
        curl -sSL -o xcaddy.deb "${LATEST_XCADDY_URL}" && \
        dpkg -i xcaddy.deb && \
        rm xcaddy.deb && \
        apt-get clean && \
        rm -rf /var/lib/apt/lists/*

    # The xcaddy binary is now available at /usr/bin/xcaddy (in the PATH).

# --- Build Stage ---
# This target compiles the custom Caddy binary using the installed xcaddy.
build:
    FROM +builder

    # Cache the Go build directory to speed up subsequent plugin compilations.
    CACHE /root/.cache/go-build

    WORKDIR /build

    # Copy the plugin source code
    COPY . .

    # Build the LATEST version of Caddy with both plugins:
    # - Cloudflare DNS plugin for ACME DNS challenges
    # - Local Tailscale auth plugin (using local source code)
    RUN xcaddy build latest \
        --with github.com/caddy-dns/cloudflare \
        --with caddyauth=.

    # Save the compiled binary as an artifact for the next stage.
    SAVE ARTIFACT ./caddy /caddy

# --- Final Target: Save Binary ---
# This target takes the compiled binary from the 'build' stage and
# saves it to your local filesystem in the current directory.
binary:
    FROM scratch
    COPY +build/caddy ./caddy
    SAVE ARTIFACT caddy AS LOCAL ./caddy