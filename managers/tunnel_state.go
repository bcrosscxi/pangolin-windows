//go:build windows

package managers

import "github.com/fosrl/windows/tunnel"

// TunnelState is an alias for tunnel.State to make it accessible from the managers package
type TunnelState = tunnel.State

// Tunnel state constants
const (
	TunnelStateStopped  = tunnel.StateStopped
	TunnelStateStarting = tunnel.StateStarting
	TunnelStateRunning  = tunnel.StateRunning
	TunnelStateStopping = tunnel.StateStopping
)
