// helpers.go - Common test helpers for integration tests

package integration

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// getFreePort returns an available port on localhost
func getFreePort(t *testing.T) int {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to get free port: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	return port
}

// buildBinary builds the jhub-app-proxy binary and returns its path
func buildBinary(t *testing.T) string {
	projectRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("Failed to get project root: %v", err)
	}

	binaryPath := filepath.Join(projectRoot, "jhub-app-proxy-test")

	cmd := exec.Command("go", "build", "-o", binaryPath, "./cmd/jhub-app-proxy")
	cmd.Dir = projectRoot
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		t.Fatalf("Failed to build binary: %v", err)
	}

	t.Cleanup(func() {
		os.Remove(binaryPath)
	})

	return binaryPath
}

// waitForHTTP waits for an HTTP endpoint to respond with 200 OK
func waitForHTTP(url string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for %s", url)
		case <-ticker.C:
			resp, err := http.Get(url)
			if err == nil {
				resp.Body.Close()
				if resp.StatusCode == 200 {
					return nil
				}
			}
		}
	}
}

// startJupyterHubContainer starts a JupyterHub testcontainer and returns the container, API URL, and hub URL
func startJupyterHubContainer(ctx context.Context, t *testing.T) (testcontainers.Container, string, string, error) {
	configPath, err := filepath.Abs("testdata/jupyterhub_config.py")
	if err != nil {
		return nil, "", "", fmt.Errorf("failed to get config path: %w", err)
	}

	req := testcontainers.ContainerRequest{
		Image:        "jupyterhub/jupyterhub:latest",
		ExposedPorts: []string{"8000/tcp", "8081/tcp"},
		WaitingFor:   wait.ForLog("JupyterHub is now running").WithStartupTimeout(60 * time.Second),
		Files: []testcontainers.ContainerFile{
			{
				HostFilePath:      configPath,
				ContainerFilePath: "/srv/jupyterhub/jupyterhub_config.py",
				FileMode:          0644,
			},
		},
		Cmd: []string{"jupyterhub", "-f", "/srv/jupyterhub/jupyterhub_config.py"},
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		return nil, "", "", fmt.Errorf("failed to start JupyterHub container: %w", err)
	}

	hubPort, err := container.MappedPort(ctx, "8000")
	if err != nil {
		container.Terminate(ctx)
		return nil, "", "", fmt.Errorf("failed to get mapped port: %w", err)
	}

	hubHost, err := container.Host(ctx)
	if err != nil {
		container.Terminate(ctx)
		return nil, "", "", fmt.Errorf("failed to get container host: %w", err)
	}

	hubURL := fmt.Sprintf("http://%s:%s", hubHost, hubPort.Port())
	hubAPIURL := fmt.Sprintf("%s/hub/api", hubURL)

	t.Logf("JupyterHub running at %s", hubURL)

	return container, hubAPIURL, hubURL, nil
}

// waitForJupyterHubAPI waits for JupyterHub API to respond
func waitForJupyterHubAPI(apiURL string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for JupyterHub API at %s", apiURL)
		case <-ticker.C:
			resp, err := http.Get(apiURL)
			if err == nil {
				resp.Body.Close()
				if resp.StatusCode == 200 {
					return nil
				}
			}
		}
	}
}
