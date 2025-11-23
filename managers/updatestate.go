//go:build windows

package managers

import (
	"os"
	"time"
	_ "unsafe"

	"github.com/fosrl/newt/logger"
	"github.com/fosrl/windows/services"
	"github.com/fosrl/windows/updater"
	"github.com/fosrl/windows/version"
)

//go:linkname fastrandn runtime.fastrandn
func fastrandn(n uint32) uint32

type UpdateState uint32

const (
	UpdateStateUnknown UpdateState = iota
	UpdateStateFoundUpdate
	UpdateStateUpdatesDisabledUnofficialBuild
)

var updateState = UpdateStateUnknown

func jitterSleep(min, max time.Duration) {
	time.Sleep(min + time.Millisecond*time.Duration(fastrandn(uint32((max-min+1)/time.Millisecond))))
}

func checkForUpdates() {
	// Check if running official version, with dev mode support
	isOfficial := version.IsRunningOfficialVersion()
	if !isOfficial {
		// Allow dev mode updates via environment variable (same as updater package)
		devMode := false
		if os.Getenv("PANGOLIN_ALLOW_DEV_UPDATES") == "1" {
			devMode = true
		}
		if !devMode {
			logger.Info("Build is not official, so updates are disabled")
			updateState = UpdateStateUpdatesDisabledUnofficialBuild
			IPCServerNotifyUpdateFound(updateState)
			return
		}
		logger.Info("Development mode enabled - allowing updates on unsigned build")
	}

	// Initial jitter if started at boot - prevents all machines from checking at once after boot
	if services.StartedAtBoot() {
		jitterSleep(time.Minute*2, time.Minute*5)
	}

	noError, didNotify := true, false
	for {
		update, err := updater.CheckForUpdate()
		if err == nil && update != nil && !didNotify {
			logger.Info("An update is available")
			updateState = UpdateStateFoundUpdate
			IPCServerNotifyUpdateFound(updateState)
			didNotify = true
		} else if err != nil && !didNotify {
			logger.Error("Update checker: %v", err)
			if noError {
				jitterSleep(time.Minute*4, time.Minute*6)
				noError = false
			} else {
				jitterSleep(time.Minute*25, time.Minute*30)
			}
		} else {
			jitterSleep(time.Hour-time.Minute*3, time.Hour+time.Minute*3)
		}
	}
}
