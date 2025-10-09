// Package port provides port allocation utilities
package port

import (
	"fmt"
	"net"
)

// Allocate finds an available port on the local machine
// Returns 0 to let the OS choose a random port if preferredPort is 0
func Allocate(preferredPort int) (int, error) {
	if preferredPort == 0 {
		// Let OS choose a random free port
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			return 0, fmt.Errorf("failed to allocate random port: %w", err)
		}
		defer listener.Close()

		addr := listener.Addr().(*net.TCPAddr)
		return addr.Port, nil
	}

	// Try the preferred port
	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", preferredPort))
	if err != nil {
		// Port not available, try random
		return Allocate(0)
	}
	defer listener.Close()

	return preferredPort, nil
}

// IsAvailable checks if a port is available for listening
func IsAvailable(port int) bool {
	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return false
	}
	listener.Close()
	return true
}
