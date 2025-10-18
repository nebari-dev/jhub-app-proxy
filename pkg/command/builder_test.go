package command

import (
	"os"
	"testing"
)

func TestGetRootPath(t *testing.T) {
	tests := []struct {
		name          string
		servicePrefix string
		expected      string
	}{
		{
			name:          "standard service prefix",
			servicePrefix: "/user/fakeuser/myapp/",
			expected:      "/hub/user/fakeuser/myapp",
		},
		{
			name:          "service prefix without trailing slash",
			servicePrefix: "/user/testuser/app",
			expected:      "/hub/user/testuser/app",
		},
		{
			name:          "service prefix without leading slash",
			servicePrefix: "user/demouser/app/",
			expected:      "/hub/user/demouser/app",
		},
		{
			name:          "empty service prefix",
			servicePrefix: "",
			expected:      "",
		},
		{
			name:          "simple service prefix",
			servicePrefix: "/user/alice/",
			expected:      "/hub/user/alice",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set environment variable
			if tt.servicePrefix != "" {
				os.Setenv("JUPYTERHUB_SERVICE_PREFIX", tt.servicePrefix)
			} else {
				os.Unsetenv("JUPYTERHUB_SERVICE_PREFIX")
			}
			defer os.Unsetenv("JUPYTERHUB_SERVICE_PREFIX")

			result := GetRootPath()
			if result != tt.expected {
				t.Errorf("GetRootPath() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestSubstitutePort(t *testing.T) {
	tests := []struct {
		name          string
		command       []string
		port          int
		servicePrefix string
		expected      []string
	}{
		{
			name:          "substitute port only",
			command:       []string{"python", "-m", "http.server", "{port}"},
			port:          8080,
			servicePrefix: "",
			expected:      []string{"python", "-m", "http.server", "8080"},
		},
		{
			name:          "substitute root_path only",
			command:       []string{"myapp", "--root-path", "{root_path}"},
			port:          8080,
			servicePrefix: "/user/test/app/",
			expected:      []string{"myapp", "--root-path", "/hub/user/test/app"},
		},
		{
			name:          "substitute both port and root_path",
			command:       []string{"myapp", "--port", "{port}", "--root-path", "{root_path}"},
			port:          9000,
			servicePrefix: "/user/bob/dashboard/",
			expected:      []string{"myapp", "--port", "9000", "--root-path", "/hub/user/bob/dashboard"},
		},
		{
			name:          "substitute dash placeholders",
			command:       []string{"myapp", "{-}p", "{port}", "{--}root-path", "{root_path}"},
			port:          8888,
			servicePrefix: "/user/test/",
			expected:      []string{"myapp", "-p", "8888", "--root-path", "/hub/user/test"},
		},
		{
			name:          "strip single quotes",
			command:       []string{"'myapp --port {port}'"},
			port:          3000,
			servicePrefix: "",
			expected:      []string{"myapp --port 3000"},
		},
		{
			name:          "strip double quotes",
			command:       []string{`"myapp --root-path {root_path}"`},
			port:          3000,
			servicePrefix: "/user/demo/",
			expected:      []string{"myapp --root-path /hub/user/demo"},
		},
		{
			name:          "empty root_path when no service prefix",
			command:       []string{"myapp", "--root-path", "{root_path}"},
			port:          5000,
			servicePrefix: "",
			expected:      []string{"myapp", "--root-path", ""},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set environment variable
			if tt.servicePrefix != "" {
				os.Setenv("JUPYTERHUB_SERVICE_PREFIX", tt.servicePrefix)
			} else {
				os.Unsetenv("JUPYTERHUB_SERVICE_PREFIX")
			}
			defer os.Unsetenv("JUPYTERHUB_SERVICE_PREFIX")

			result := SubstitutePort(tt.command, tt.port)
			if len(result) != len(tt.expected) {
				t.Fatalf("SubstitutePort() returned %d args, want %d", len(result), len(tt.expected))
			}
			for i := range result {
				if result[i] != tt.expected[i] {
					t.Errorf("SubstitutePort()[%d] = %q, want %q", i, result[i], tt.expected[i])
				}
			}
		})
	}
}
