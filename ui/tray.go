//go:build windows

package ui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"unsafe"

	"github.com/fosrl/windows/config"
	"github.com/fosrl/windows/managers"
	"github.com/fosrl/windows/updater"
	"github.com/fosrl/windows/version"

	"github.com/fosrl/newt/logger"
	"github.com/tailscale/walk"
	"github.com/tailscale/win"
)

var (
	trayIcon            *walk.NotifyIcon
	contextMenu         *walk.Menu
	mainWindow          *walk.MainWindow
	hasUpdate           bool
	updateMutex         sync.RWMutex
	updateAction        *walk.Action // Action for "Update Available" menu item
	labelAction         *walk.Action
	loginAction         *walk.Action
	connectAction       *walk.Action
	moreAction          *walk.Action
	quitAction          *walk.Action
	updateFoundCb       *managers.UpdateFoundCallback
	updateProgressCb    *managers.UpdateProgressCallback
	managerStoppingCb   *managers.ManagerStoppingCallback
	tunnelStateChangeCb *managers.TunnelStateChangeCallback
	isConnected         bool
	connectMutex        sync.RWMutex
)

// setTrayIcon updates the tray icon based on connection status
// connected: true for orange icon, false for gray icon
func setTrayIcon(connected bool) {
	if trayIcon == nil {
		return
	}

	var iconName string
	if connected {
		iconName = "icon-orange.ico"
	} else {
		iconName = "icon-gray.ico"
	}

	iconPath := filepath.Join(config.GetIconsPath(), iconName)
	icon, err := walk.NewIconFromFile(iconPath)
	if err != nil {
		logger.Error("Failed to load icon from %s: %v", iconPath, err)
		// Fallback to system icon
		icon, err = walk.NewIconFromResourceId(32517) // IDI_INFORMATION
		if err != nil {
			logger.Error("Failed to load fallback icon: %v", err)
			return
		}
	}

	if icon != nil {
		if err := trayIcon.SetIcon(icon); err != nil {
			logger.Error("Failed to set tray icon: %v", err)
		}
	}
}

func SetupTray(mw *walk.MainWindow) error {
	// Store references for update menu management
	mainWindow = mw

	// Create NotifyIcon
	ni, err := walk.NewNotifyIcon()
	if err != nil {
		return err
	}
	trayIcon = ni // Store reference for icon updates

	// Load default gray icon (disconnected state)
	setTrayIcon(false)

	// Set tooltip
	ni.SetToolTip(config.AppName)

	// Create grayed out label action
	labelAction = walk.NewAction()
	labelAction.SetText("milo@pangolin.net")
	labelAction.SetEnabled(false) // Gray out the text

	// Create Login action
	loginAction = walk.NewAction()
	loginAction.SetText("Login")
	loginAction.Triggered().Attach(func() {
		ShowLoginDialog(mw)
	})

	// Create Connect action (toggle button with checkmark)
	connectAction = walk.NewAction()
	connectAction.SetText("Connect")
	connectAction.SetChecked(false) // Initially unchecked
	connectAction.Triggered().Attach(func() {
		go func() {
			connectMutex.RLock()
			currentState := isConnected
			connectMutex.RUnlock()

			if currentState {
				// Disconnect
				logger.Info("Disconnecting...")
				err := managers.IPCClientStopTunnel()
				if err != nil {
					logger.Error("Failed to stop tunnel: %v", err)
					walk.App().Synchronize(func() {
						connectAction.SetChecked(true) // Revert on error
					})
				}
			} else {
				// Connect - create typed config struct
				config := managers.TunnelConfig{
					Name:      "pangolin-tunnel",
					Endpoint:  "example.pangolin.net:51820",
					DNS:       "8.8.8.8,1.1.1.1",
					Address:   "10.0.0.2/24",
					UserToken: "abc123",
				}
				logger.Info("Connecting with config: Name=%s, Endpoint=%s", config.Name, config.Endpoint)
				err := managers.IPCClientStartTunnel(config)
				if err != nil {
					logger.Error("Failed to start tunnel: %v", err)
					walk.App().Synchronize(func() {
						connectAction.SetChecked(false) // Revert on error
					})
				}
			}
		}()
	})

	// Create More submenu with Documentation and Open Logs
	moreMenu, err := walk.NewMenu()
	if err != nil {
		return err
	}
	docAction := walk.NewAction()
	docAction.SetText("Documentation")
	docAction.Triggered().Attach(func() {
		url := "https://github.com/tailscale/walk"
		cmd := exec.Command("cmd", "/c", "start", url)
		if err := cmd.Run(); err != nil {
			logger.Error("Failed to open documentation: %v", err)
		}
	})
	moreMenu.Actions().Add(docAction)

	openLogsAction := walk.NewAction()
	openLogsAction.SetText("Open Logs Location")
	openLogsAction.Triggered().Attach(func() {
		logDir := config.GetLogDir()
		// Ensure the directory exists
		if err := os.MkdirAll(logDir, 0755); err != nil {
			logger.Error("Failed to create log directory: %v", err)
		}
		// Open the directory in Windows Explorer
		cmd := exec.Command("explorer", logDir)
		if err := cmd.Run(); err != nil {
			logger.Error("Failed to open log directory: %v", err)
		}
	})
	moreMenu.Actions().Add(openLogsAction)

	// Create Check for Updates action
	checkUpdateAction := walk.NewAction()
	checkUpdateAction.SetText("Check for Updates")
	checkUpdateAction.Triggered().Attach(func() {
		go func() {
			logger.Info("Checking for updates via manager...")
			logger.Info("Current version: %s", version.Number)

			// Check update state via manager IPC
			updateState, err := managers.IPCClientUpdateState()
			if err != nil {
				logger.Error("Update check failed: %v", err)
				walk.App().Synchronize(func() {
					td := walk.NewTaskDialog()
					_, _ = td.Show(walk.TaskDialogOpts{
						Owner:         mw,
						Title:         "Update Check Failed",
						Content:       fmt.Sprintf("Failed to check for updates: %v", err),
						IconSystem:    walk.TaskDialogSystemIconError,
						CommonButtons: win.TDCBF_OK_BUTTON,
					})
				})
				return
			}

			switch updateState {
			case managers.UpdateStateFoundUpdate:
				logger.Info("Update available")
				// Trigger the update
				triggerUpdate(mw)
			case managers.UpdateStateUpdatesDisabledUnofficialBuild:
				walk.App().Synchronize(func() {
					td := walk.NewTaskDialog()
					_, _ = td.Show(walk.TaskDialogOpts{
						Owner:         mw,
						Title:         "Updates Disabled",
						Content:       "Updates are disabled for unofficial builds.",
						IconSystem:    walk.TaskDialogSystemIconInformation,
						CommonButtons: win.TDCBF_OK_BUTTON,
					})
				})
			default:
				logger.Info("No update available")
				walk.App().Synchronize(func() {
					td := walk.NewTaskDialog()
					_, _ = td.Show(walk.TaskDialogOpts{
						Owner:         mw,
						Title:         "No Update Available",
						Content:       "You are running the latest version.",
						IconSystem:    walk.TaskDialogSystemIconInformation,
						CommonButtons: win.TDCBF_OK_BUTTON,
					})
				})
			}
		}()
	})
	moreMenu.Actions().Add(checkUpdateAction)

	// Add version info at the bottom, grayed out
	versionAction := walk.NewAction()
	versionAction.SetText(fmt.Sprintf("Version %s", version.Number))
	versionAction.SetEnabled(false) // Gray out the text
	moreMenu.Actions().Add(versionAction)

	moreAction = walk.NewMenuAction(moreMenu)
	moreAction.SetText("More")

	// Create Quit action
	quitAction = walk.NewAction()
	quitAction.SetText("Quit")
	quitAction.Triggered().Attach(func() {
		// Try to quit the manager service (stops tunnels and quits manager)
		// This only works if we're connected via IPC
		go func() {
			alreadyQuit, err := managers.IPCClientQuit(true) // true = stop tunnels on quit
			if err != nil {
				logger.Error("Failed to quit manager service: %v", err)
			} else if alreadyQuit {
				logger.Info("Manager service already quitting")
			} else {
				logger.Info("Manager service quit requested")
			}
		}()
		// Exit the UI - if connected via IPC, manager will also stop
		walk.App().Exit(0)
	})

	// Initialize context menu and add all initial actions
	contextMenu = ni.ContextMenu()
	contextMenu.Actions().Add(labelAction) // Add label first (grayed out)
	contextMenu.Actions().Add(loginAction) // Add Login button
	contextMenu.Actions().Add(connectAction)
	contextMenu.Actions().Add(moreAction)
	contextMenu.Actions().Add(quitAction)

	// Handle left-click to show popup menu using Windows API
	ni.MouseDown().Attach(func(x, y int, button walk.MouseButton) {
		if button == walk.LeftButton {
			// Get cursor position
			var pt win.POINT
			win.GetCursorPos(&pt)

			// Get the menu handle from the context menu using unsafe
			// The Menu struct should have an hMenu field as the first field
			menuPtr := (*struct {
				hMenu win.HMENU
			})(unsafe.Pointer(contextMenu))

			if menuPtr.hMenu != 0 {
				// Show the menu using TrackPopupMenu
				// TrackPopupMenu automatically closes when clicking away
				// We need to set the window as foreground to ensure proper message handling
				win.SetForegroundWindow(mw.Handle())
				win.TrackPopupMenu(
					menuPtr.hMenu,
					win.TPM_LEFTALIGN|win.TPM_LEFTBUTTON|win.TPM_RIGHTBUTTON,
					pt.X,
					pt.Y,
					0,
					mw.Handle(),
					nil,
				)
				// Post a null message to ensure the menu closes properly
				win.PostMessage(mw.Handle(), win.WM_NULL, 0, 0)
			}
		}
	})

	ni.SetVisible(true)

	// Register for update notifications from manager (if connected via IPC)
	// These callbacks will be called when the manager finds updates or makes progress
	updateFoundCb = managers.IPCClientRegisterUpdateFound(func(updateState managers.UpdateState) {
		if updateState == managers.UpdateStateFoundUpdate {
			updateMutex.Lock()
			hasUpdate = true
			updateMutex.Unlock()
			updateMenuWithAvailableUpdate()
		} else {
			updateMutex.Lock()
			hasUpdate = false
			updateMutex.Unlock()
			updateMenuWithAvailableUpdate()
		}
	})

	// Register for manager stopping notification
	managerStoppingCb = managers.IPCClientRegisterManagerStopping(func() {
		logger.Info("Manager service is stopping, exiting UI")
		walk.App().Synchronize(func() {
			walk.App().Exit(0)
		})
	})

	updateProgressCb = managers.IPCClientRegisterUpdateProgress(func(dp updater.DownloadProgress) {
		if dp.Error != nil {
			logger.Error("Update error: %v", dp.Error)
			walk.App().Synchronize(func() {
				td := walk.NewTaskDialog()
				_, _ = td.Show(walk.TaskDialogOpts{
					Owner:         mw,
					Title:         "Update Failed",
					Content:       fmt.Sprintf("Update failed: %v", dp.Error),
					IconSystem:    walk.TaskDialogSystemIconError,
					CommonButtons: win.TDCBF_OK_BUTTON,
				})
			})
			return
		}

		if len(dp.Activity) > 0 {
			logger.Info("Update: %s", dp.Activity)
		}

		if dp.BytesTotal > 0 {
			percent := float64(dp.BytesDownloaded) / float64(dp.BytesTotal) * 100
			logger.Info("Download progress: %.1f%% (%d/%d bytes)", percent, dp.BytesDownloaded, dp.BytesTotal)
		}

		if dp.Complete {
			logger.Info("Update complete! The application will restart.")
			walk.App().Synchronize(func() {
				td := walk.NewTaskDialog()
				_, _ = td.Show(walk.TaskDialogOpts{
					Owner:         mw,
					Title:         "Update Complete",
					Content:       "The update has been installed successfully. The application will now restart.",
					IconSystem:    walk.TaskDialogSystemIconInformation,
					CommonButtons: win.TDCBF_OK_BUTTON,
				})
			})
			// Clear the update after installation starts
			updateMutex.Lock()
			hasUpdate = false
			updateMutex.Unlock()
			updateMenuWithAvailableUpdate()
			// The MSI installer will handle the restart
		}
	})

	// Check initial update state
	go func() {
		updateState, err := managers.IPCClientUpdateState()
		if err == nil && updateState == managers.UpdateStateFoundUpdate {
			updateMutex.Lock()
			hasUpdate = true
			updateMutex.Unlock()
			updateMenuWithAvailableUpdate()
		}
	}()

	// Register for tunnel state change notifications
	tunnelStateChangeCb = managers.IPCClientRegisterTunnelStateChange(func(state managers.TunnelState) {
		logger.Info("Tunnel state changed: %s", state.String())
		walk.App().Synchronize(func() {
			switch state {
			case managers.TunnelStateRunning:
				connectMutex.Lock()
				isConnected = true
				connectMutex.Unlock()
				connectAction.SetChecked(true)
				setTrayIcon(true)
				connectAction.SetText("Disconnect")
			case managers.TunnelStateStopped:
				connectMutex.Lock()
				isConnected = false
				connectMutex.Unlock()
				connectAction.SetChecked(false)
				setTrayIcon(false)
				connectAction.SetText("Connect")
			case managers.TunnelStateStarting:
				connectAction.SetText("Connecting...")
			case managers.TunnelStateStopping:
				connectAction.SetText("Disconnecting...")
			}
		})
	})

	return nil
}

// triggerUpdate asks the user for confirmation and then triggers the update via manager
func triggerUpdate(mw *walk.MainWindow) {
	userAcceptedChan := make(chan bool, 1)

	// Show dialog on UI thread - Show() blocks until dialog is closed
	walk.App().Synchronize(func() {
		td := walk.NewTaskDialog()
		opts := walk.TaskDialogOpts{
			Owner:         mw,
			Title:         "Update Available",
			Content:       "A new version is available.\n\nWould you like to download and install it now?",
			IconSystem:    walk.TaskDialogSystemIconInformation,
			CommonButtons: win.TDCBF_YES_BUTTON | win.TDCBF_NO_BUTTON,
			DefaultButton: walk.TaskDialogDefaultButtonYes,
		}
		opts.CommonButtonClicked(win.TDCBF_YES_BUTTON).Attach(func() bool {
			select {
			case userAcceptedChan <- true:
			default:
			}
			return false // Return false to allow dialog to close normally
		})
		opts.CommonButtonClicked(win.TDCBF_NO_BUTTON).Attach(func() bool {
			select {
			case userAcceptedChan <- false:
			default:
			}
			return false // Return false to allow dialog to close normally
		})
		td.Show(opts)
	})

	// Wait for user response
	userAccepted := <-userAcceptedChan
	if !userAccepted {
		logger.Info("User declined update")
		return
	}

	// Trigger update via manager IPC
	logger.Info("Starting update download via manager...")
	err := managers.IPCClientUpdate()
	if err != nil {
		logger.Error("Failed to trigger update: %v", err)
		walk.App().Synchronize(func() {
			td := walk.NewTaskDialog()
			td.Show(walk.TaskDialogOpts{
				Owner:         mw,
				Title:         "Update Failed",
				Content:       fmt.Sprintf("Failed to start update: %v", err),
				IconSystem:    walk.TaskDialogSystemIconError,
				CommonButtons: win.TDCBF_OK_BUTTON,
			})
		})
	}
}

// updateMenuWithAvailableUpdate adds or removes the "Update Available" menu item
// based on whether an update is available. Uses Insert/Remove like WireGuard does.
func updateMenuWithAvailableUpdate() {
	if contextMenu == nil {
		return
	}

	// Safely get the app instance - it might not be ready yet
	app := walk.App()
	if app == nil {
		logger.Error("Cannot update menu: walk.App() is nil (app not initialized)")
		return
	}

	updateMutex.RLock()
	hasUpdateLocal := hasUpdate
	updateMutex.RUnlock()

	// Use defer/recover to catch any panics from Synchronize
	defer func() {
		if r := recover(); r != nil {
			logger.Error("Panic in updateMenuWithAvailableUpdate: %v", r)
		}
	}()

	app.Synchronize(func() {
		// Recover from any panics that occur on the UI thread
		defer func() {
			if r := recover(); r != nil {
				logger.Error("Panic in Synchronize callback (UI thread): %v", r)
			}
		}()

		actions := contextMenu.Actions()

		// Check if update action is already in the menu
		updateActionInMenu := false
		if updateAction != nil {
			for i := 0; i < actions.Len(); i++ {
				if actions.At(i) == updateAction {
					updateActionInMenu = true
					break
				}
			}
		}

		if hasUpdateLocal {
			// Create update menu item if it doesn't exist
			if updateAction == nil {
				updateAction = walk.NewAction()
				updateAction.SetText("Update available")
				updateAction.Triggered().Attach(func() {
					// Run in goroutine to avoid blocking the menu action handler
					go triggerUpdate(mainWindow)
				})
			} else {
				// Update the text if action already exists (keep it simple)
				updateAction.SetText("Update available")
			}

			// Insert update action if it's not already in the menu
			// Insert after connectAction (before moreAction)
			if !updateActionInMenu {
				// Find the index of moreAction to insert before it
				moreActionIndex := -1
				for i := 0; i < actions.Len(); i++ {
					if actions.At(i) == moreAction {
						moreActionIndex = i
						break
					}
				}
				if moreActionIndex >= 0 {
					actions.Insert(moreActionIndex, updateAction)
				} else {
					// Fallback: just add it
					actions.Add(updateAction)
				}
			}
		} else {
			// Remove update action if it exists in the menu
			if updateActionInMenu && updateAction != nil {
				actions.Remove(updateAction)
			}
			// Note: We don't set updateAction to nil here because we want to keep
			// the action object for potential reuse, just remove it from the menu
		}
	})
}
