//go:build windows

package tunnel

import (
	"github.com/fosrl/newt/logger"
)

// destroyTunnel performs cleanup and tears down the tunnel
// This should be called before the service stops to ensure clean shutdown
func destroyTunnel(config Config) {
	// TODO: Implement actual tunnel destruction logic
	logger.Info("Tunnel: Destroying tunnel for %s", config.Name)
	// print config
	logger.Info("Tunnel: Config: %+v", config)
}
