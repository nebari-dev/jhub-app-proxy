# JHub Apps Proxy

[![Test and Build](https://github.com/nebari-dev/jhub-app-proxy/actions/workflows/test.yml/badge.svg)](https://github.com/nebari-dev/jhub-app-proxy/actions/workflows/test.yml)
[![Release](https://github.com/nebari-dev/jhub-app-proxy/actions/workflows/release.yml/badge.svg)](https://github.com/nebari-dev/jhub-app-proxy/actions/workflows/release.yml)
[![Install Script Test](https://github.com/nebari-dev/jhub-app-proxy/actions/workflows/install-test.yml/badge.svg)](https://github.com/nebari-dev/jhub-app-proxy/actions/workflows/install-test.yml)
[![Go Version](https://img.shields.io/github/go-mod/go-version/nebari-dev/jhub-app-proxy?style=flat-square)](https://go.dev/)
[![License](https://img.shields.io/github/license/nebari-dev/jhub-app-proxy?style=flat-square)](LICENSE)
[![Release](https://img.shields.io/github/v/release/nebari-dev/jhub-app-proxy?style=flat-square)](https://github.com/nebari-dev/jhub-app-proxy/releases)

> **⚠️ Early Alpha Release**
>
> This project is in early alpha stage. While functional, the API and features may change. Use with caution in production environments and expect potential breaking changes in future releases.

A lightweight app proxy for JupyterHub applications that displays real-time startup logs before forwarding traffic to your app.

**Note:** This project is the successor to [jhsingle-native-proxy](https://github.com/ideonate/jhsingle-native-proxy), rewritten in Go for improved portability, observability, and maintainability.

![JHub Apps Proxy UI](ui-screenshot.png)

## Features

- **Real-time log viewing** - See your app's startup logs in a web interface
- **Smart proxying** - Automatically forwards traffic once your app is ready
- **Process management** - Handles conda environments, git repositories, and custom commands
- **Health checking** - Configurable health checks to determine when your app is ready
- **Zero downtime** - JupyterHub redirects users immediately to the log viewer

## Installation

### Quick Install (recommended)

```bash
curl -fsSL https://raw.githubusercontent.com/nebari-dev/jhub-app-proxy/main/install.sh | bash
```

Or with specific version:

```bash
curl -fsSL https://raw.githubusercontent.com/nebari-dev/jhub-app-proxy/main/install.sh | bash -s -- -v v0.1
```

### Manual Install

Download and run the install script:

```bash
wget https://raw.githubusercontent.com/nebari-dev/jhub-app-proxy/main/install.sh
chmod +x install.sh
./install.sh              # Install latest version
./install.sh -v v0.1      # Install specific version
./install.sh -d /usr/local/bin  # Custom install directory
```

### Alternative Methods

```bash
go install github.com/nebari-dev/jhub-app-proxy/cmd/jhub-app-proxy@latest
```

Or build from source:

```bash
make build
```

## Usage

Basic example (local testing without authentication):

```bash
jhub-app-proxy --port 8000 --destport 3000 --authtype none --log-format pretty \
  -- python -m http.server 3000
```

With conda environment:

```bash
jhub-app-proxy --port 8000 --destport 8501 --authtype none --log-format pretty \
  --conda-env my-env \
  -- streamlit run app.py
```

With git repository:

```bash
jhub-app-proxy --port 8000 --destport 8050 --authtype none --log-format pretty \
  --repo https://github.com/nebari-dev/jhub-apps-from-git-repo-example \
  --conda-env dashapp \
  -- python app.py
```

**Note:** For local testing, use `--authtype none` and `--log-format pretty` for readable logs. In JupyterHub deployments, use `--authtype oauth` (default) which requires `JUPYTERHUB_API_URL` and related environment variables to be set.

## How It Works

1. User clicks "Launch App" in JupyterHub
2. JHub Apps Proxy starts and immediately shows a log viewer (200 status)
3. JupyterHub redirects the user to see real-time startup logs
4. Once the app passes health checks, traffic is proxied to your application
5. User never sees a timeout or loading spinner

## Configuration

### Core Flags
- `--port` - Port for proxy server to listen on (default: 8888)
- `--destport` - Internal subprocess port (0 = random, default: 0)
- `--authtype` - Authentication type: `oauth`, `none` (default: `oauth`)

### Template Substitution

JHub Apps Proxy supports template placeholders in your application commands that are automatically replaced at runtime:

#### Port Templating
Use `{port}` in your command and it will be replaced with the actual port allocated for internal routing:

```bash
jhub-app-proxy --port 8000 --destport 8501 --authtype none \
  -- streamlit run app.py --server.port {port}
```

#### Root Path Templating
Use `{root_path}` when your application needs to know its deployment prefix. This is especially useful when apps are deployed at dynamic URLs that aren't known in advance.

The `{root_path}` placeholder is automatically replaced with the appropriate path constructed from the `JUPYTERHUB_SERVICE_PREFIX` environment variable (prepended with `/hub`).

**Example:** If `JUPYTERHUB_SERVICE_PREFIX=/user/alice@example.com/myapp/`, then `{root_path}` becomes `/hub/user/alice@example.com/myapp`

```bash
jhub-app-proxy --port 8000 --destport 8050 --authtype none \
  -- panel serve app.py --port {port} --prefix {root_path}
```

This eliminates the need to hardcode deployment paths in your application commands, making them portable across different JupyterHub deployments.

### Process Management
- `--conda-env` - Conda environment to activate before running command
- `--workdir` - Working directory for the process
- `--keep-alive` - Always report activity to prevent idle culling (default: `false`)

### Git Repository
- `--repo` - Git repository URL to clone before starting app
- `--repofolder` - Destination folder for git clone
- `--repobranch` - Git branch to checkout (default: `main`)

### Health Check
- `--ready-check-path` - Health check URL path (default: `/`)
- `--ready-timeout` - Health check timeout in seconds (default: 300)

### Logging
- `--log-level` - Log level: `debug`, `info`, `warn`, `error` (default: `info`)
- `--log-format` - Log format: `json`, `pretty` (default: `json`)
- `--log-buffer-size` - Number of subprocess log lines to keep in memory (default: 1000)
- `--log-caller` - Show file:line in logs (default: `false`)

### Progressive Streaming
- `--progressive` - Enable progressive response streaming, useful for Voila to show results as they're computed (default: `false`)

