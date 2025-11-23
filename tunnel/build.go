//go:build windows

package tunnel

import (
	"github.com/fosrl/newt/logger"
)

// buildTunnel builds the tunnel
func buildTunnel(config Config) error {
	// TODO: Implement actual tunnel building logic
	logger.Info("Tunnel: Building tunnel for %s", config.Name)
	// print config
	logger.Info("Tunnel: Config: %+v", config)
	return nil
}
