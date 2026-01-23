//go:build windows

package version

import (
	"fmt"
	"runtime"
)

func Arch() string {
	switch runtime.GOARCH {
	case "arm", "arm64", "amd64":
		return runtime.GOARCH
	case "386":
		return "x86"
	default:
		panic("Unrecognized GOARCH")
	}
}

func UserAgent() string {
	return fmt.Sprintf("pangolin-windows-%s", Number)
}
