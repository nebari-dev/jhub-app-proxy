# JHub Apps Proxy

A lightweight app proxy for JupyterHub applications that displays real-time startup logs before forwarding traffic to your app.

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

```bash
jhub-app-proxy --port 8000 --upstream http://localhost:3000 \
  -- python -m http.server 3000
```

With conda environment:

```bash
jhub-app-proxy --port 8000 --upstream http://localhost:8501 \
  --conda-env my-env \
  -- streamlit run app.py
```

With git repository:

```bash
jhub-app-proxy --port 8000 --upstream http://localhost:8050 \
  --git-url https://github.com/user/dash-app.git \
  --conda-env dashapp \
  -- python app.py
```

## How It Works

1. User clicks "Launch App" in JupyterHub
2. JHub Apps Proxy starts and immediately shows a log viewer (200 status)
3. JupyterHub redirects the user to see real-time startup logs
4. Once the app passes health checks, traffic is proxied to your application
5. User never sees a timeout or loading spinner

## Configuration

- `--port` - Port to listen on (default: 8000)
- `--upstream` - URL of the application to proxy to
- `--conda-env` - Conda environment to activate
- `--git-url` - Git repository to clone
- `--health-check-url` - Custom health check endpoint
- `--health-check-timeout` - Health check timeout (default: 5m)

