//go:build windows

package ui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"unsafe"

	"github.com/fosrl/windows/api"
	"github.com/fosrl/windows/auth"
	"github.com/fosrl/windows/config"

	"github.com/fosrl/newt/logger"
	"github.com/tailscale/walk"
	. "github.com/tailscale/walk/declarative"
	"github.com/tailscale/win"
	"golang.org/x/sys/windows"
)

type hostingOption int

const (
	hostingNone hostingOption = iota
	hostingCloud
	hostingSelfHosted
)

type loginState int

const (
	stateHostingSelection loginState = iota
	stateReadyToLogin
	stateDeviceAuthCode
	stateSuccess
)

// getIconsPath returns the path to the icons directory
// Checks relative to executable first (for development), then falls back to installed location
func getIconsPath() string {
	// Try relative to executable first (for development)
	exePath, err := os.Executable()
	if err == nil {
		exeDir := filepath.Dir(exePath)
		iconsPath := filepath.Join(exeDir, "icons")
		if _, err := os.Stat(iconsPath); err == nil {
			return iconsPath
		}
		// Also try parent directory (if running from build/)
		parentIconsPath := filepath.Join(filepath.Dir(exeDir), "icons")
		if _, err := os.Stat(parentIconsPath); err == nil {
			return parentIconsPath
		}
	}
	// Fall back to installed location
	return config.GetIconsPath()
}

// isDarkMode detects if Windows is in dark mode
func isDarkMode() bool {
	var key windows.Handle
	keyPath := windows.StringToUTF16Ptr(`Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`)
	err := windows.RegOpenKeyEx(windows.HKEY_CURRENT_USER, keyPath, 0, windows.KEY_READ, &key)
	if err != nil {
		// Default to light mode if we can't detect
		return false
	}
	defer windows.RegCloseKey(key)

	var value uint32
	var valueLen uint32 = 4
	valueName := windows.StringToUTF16Ptr("AppsUseLightTheme")
	err = windows.RegQueryValueEx(key, valueName, nil, nil, (*byte)(unsafe.Pointer(&value)), &valueLen)
	if err != nil {
		// Default to light mode if we can't read the value
		return false
	}

	// AppsUseLightTheme: 0 = dark mode, 1 = light mode
	return value == 0
}

// ShowLoginDialog shows the login dialog with full authentication flow
func ShowLoginDialog(parent walk.Form, authManager *auth.AuthManager, configManager *config.ConfigManager, apiClient *api.APIClient) {
	var dlg *walk.Dialog
	var contentComposite *walk.Composite
	var buttonComposite *walk.Composite

	// State variables
	currentState := stateHostingSelection
	hostingOpt := hostingNone
	selfHostedURL := ""
	isLoggingIn := false
	hasAutoOpenedBrowser := false

	// UI components
	var cloudButton, selfHostedButton *walk.PushButton
	var urlLabel, hintLabel *walk.Label
	var urlLineEdit *walk.LineEdit
	var codeLabel *walk.Label
	var copyButton, openBrowserButton *walk.PushButton
	var manualURLLabel *walk.Label
	var progressBar *walk.ProgressBar
	var successLabel *walk.Label
	var backButton, cancelButton, loginButton *walk.PushButton
	var logoContainer *walk.Composite

	isReadyToLogin := func() bool {
		switch hostingOpt {
		case hostingCloud:
			return true
		case hostingSelfHosted:
			return strings.TrimSpace(selfHostedURL) != ""
		default:
			return false
		}
	}

	updateButtons := func() {
		walk.App().Synchronize(func() {
			showBack := currentState != stateHostingSelection && currentState != stateSuccess
			showCancel := currentState != stateSuccess
			showLogin := currentState == stateReadyToLogin

			if backButton != nil {
				backButton.SetVisible(showBack)
				backButton.SetEnabled(!isLoggingIn)
			}
			if cancelButton != nil {
				cancelButton.SetVisible(showCancel)
				cancelButton.SetEnabled(!isLoggingIn)
			}
			if loginButton != nil {
				loginButton.SetVisible(showLogin)
				loginButton.SetEnabled(!isLoggingIn && isReadyToLogin())
			}
		})
	}

	updateUI := func() {
		walk.App().Synchronize(func() {
			// Show/hide widgets based on state
			showHostingSelection := currentState == stateHostingSelection
			showReadyToLogin := currentState == stateReadyToLogin
			showDeviceAuthCode := currentState == stateDeviceAuthCode
			showSuccess := currentState == stateSuccess

			if cloudButton != nil {
				cloudButton.SetVisible(showHostingSelection)
			}
			if selfHostedButton != nil {
				selfHostedButton.SetVisible(showHostingSelection)
			}

			if urlLabel != nil {
				urlLabel.SetVisible(showReadyToLogin)
			}
			if urlLineEdit != nil {
				urlLineEdit.SetVisible(showReadyToLogin)
			}
			if hintLabel != nil {
				hintLabel.SetVisible(showReadyToLogin)
			}

			if codeLabel != nil {
				codeLabel.SetVisible(showDeviceAuthCode)
			}
			if copyButton != nil {
				copyButton.SetVisible(showDeviceAuthCode)
			}
			if openBrowserButton != nil {
				openBrowserButton.SetVisible(showDeviceAuthCode)
			}
			if manualURLLabel != nil {
				manualURLLabel.SetVisible(showDeviceAuthCode)
			}
			if progressBar != nil {
				progressBar.SetVisible(showDeviceAuthCode)
			}

			if successLabel != nil {
				successLabel.SetVisible(showSuccess)
			}

			// Update buttons
			updateButtons()
		})
	}

	updateCodeDisplay := func() {
		walk.App().Synchronize(func() {
			code := authManager.DeviceAuthCode()
			if code != nil && codeLabel != nil {
				// Display code with spaces between characters (PIN style)
				codeStr := *code
				displayCode := strings.Join(strings.Split(codeStr, ""), " ")
				codeLabel.SetText(displayCode)

				// Auto-open browser when code is generated
				if !hasAutoOpenedBrowser {
					hasAutoOpenedBrowser = true
					hostname := configManager.GetHostname()
					if hostname != "" {
						// Remove middle hyphen from code (e.g., "XXXX-XXXX" -> "XXXXXXXX")
						codeWithoutHyphen := strings.ReplaceAll(codeStr, "-", "")
						autoOpenURL := fmt.Sprintf("%s/auth/login/device?code=%s", hostname, codeWithoutHyphen)
						openBrowser(autoOpenURL)
					}
				}
			}
		})
	}

	performLogin := func() {
		// Ensure server URL is configured
		if hostingOpt == hostingSelfHosted {
			url := strings.TrimSpace(selfHostedURL)
			if url == "" {
				walk.App().Synchronize(func() {
					isLoggingIn = false
					currentState = stateReadyToLogin
					updateUI()
					td := walk.NewTaskDialog()
					td.Show(walk.TaskDialogOpts{
						Owner:         dlg,
						Title:         "Error",
						Content:       "Please enter a server URL.",
						IconSystem:    walk.TaskDialogSystemIconError,
						CommonButtons: win.TDCBF_OK_BUTTON,
					})
				})
				return
			}
			cfg := configManager.GetConfig()
			if cfg == nil {
				cfg = &config.Config{}
			}
			cfg.Hostname = &url
			configManager.Save(cfg)
			apiClient.UpdateBaseURL(url)
		}

		err := authManager.LoginWithDeviceAuth()
		if err != nil {
			walk.App().Synchronize(func() {
				isLoggingIn = false
				errorMsg := err.Error()
				td := walk.NewTaskDialog()
				td.Show(walk.TaskDialogOpts{
					Owner:         dlg,
					Title:         "Login Error",
					Content:       errorMsg,
					IconSystem:    walk.TaskDialogSystemIconError,
					CommonButtons: win.TDCBF_OK_BUTTON,
				})
				currentState = stateReadyToLogin
				if hostingOpt == hostingCloud {
					currentState = stateHostingSelection
					hostingOpt = hostingNone
				}
				updateUI()
			})
			return
		}

		// Success - show success view, then close after 2 seconds
		walk.App().Synchronize(func() {
			currentState = stateSuccess
			isLoggingIn = false
			updateUI()

			// Close window after 2 seconds
			go func() {
				time.Sleep(2 * time.Second)
				walk.App().Synchronize(func() {
					dlg.Accept()
				})
			}()
		})
	}

	Dialog{
		AssignTo: &dlg,
		Title:    "Login",
		MinSize:  Size{Width: 450, Height: 400},
		MaxSize:  Size{Width: 450, Height: 400},
		Layout:   VBox{MarginsZero: true, Spacing: 10},
		Children: []Widget{
			// Logo container at top
			Composite{
				AssignTo: &logoContainer,
				Layout:   HBox{MarginsZero: true, Alignment: AlignHCenterVNear},
				MinSize:  Size{Width: 0, Height: 60},
			},
			// Content area
			Composite{
				AssignTo: &contentComposite,
				Layout:   VBox{MarginsZero: true, Alignment: AlignHCenterVCenter, Spacing: 12},
				MinSize:  Size{Width: 0, Height: 250},
				Children: []Widget{
					// Hosting selection buttons
					PushButton{
						AssignTo: &cloudButton,
						Text:     "Pangolin Cloud\napp.pangolin.net",
						MinSize:  Size{Width: 300, Height: 60},
						OnClicked: func() {
							hostingOpt = hostingCloud
							// Set cloud hostname
							cfg := configManager.GetConfig()
							if cfg == nil {
								cfg = &config.Config{}
							}
							hostname := "https://app.pangolin.net"
							cfg.Hostname = &hostname
							configManager.Save(cfg)
							apiClient.UpdateBaseURL(hostname)

							// Immediately start device auth flow for cloud
							currentState = stateDeviceAuthCode
							isLoggingIn = true
							updateUI()
							go performLogin()
						},
					},
					PushButton{
						AssignTo: &selfHostedButton,
						Text:     "Self-hosted or dedicated instance\nEnter your custom hostname",
						MinSize:  Size{Width: 300, Height: 60},
						OnClicked: func() {
							hostingOpt = hostingSelfHosted
							currentState = stateReadyToLogin
							// Prefill with saved hostname if it exists and is not cloud
							savedHostname := configManager.GetHostname()
							if savedHostname != "" && savedHostname != "https://app.pangolin.net" {
								selfHostedURL = savedHostname
								if urlLineEdit != nil {
									urlLineEdit.SetText(selfHostedURL)
								}
							}
							updateUI()
						},
					},
					// Self-hosted URL input
					Label{
						AssignTo:  &urlLabel,
						Text:      "Pangolin Server URL",
						Alignment: AlignHCenterVNear,
						Visible:   false,
					},
					LineEdit{
						AssignTo: &urlLineEdit,
						Text:     selfHostedURL,
						MinSize:  Size{Width: 300, Height: 0},
						Visible:  false,
						OnTextChanged: func() {
							if urlLineEdit != nil {
								selfHostedURL = urlLineEdit.Text()
								// Update config and API client as user types
								cfg := configManager.GetConfig()
								if cfg == nil {
									cfg = &config.Config{}
								}
								if selfHostedURL != "" {
									cfg.Hostname = &selfHostedURL
									configManager.Save(cfg)
									apiClient.UpdateBaseURL(selfHostedURL)
								}
								updateButtons()
							}
						},
					},
					Label{
						AssignTo:  &hintLabel,
						Text:      "Enter your Pangolin server URL",
						Alignment: AlignHCenterVNear,
						Visible:   false,
					},
					// Device auth code display
					Label{
						AssignTo:  &codeLabel,
						Text:      "",
						Alignment: AlignHCenterVNear,
						Font:      Font{PointSize: 24, Bold: true},
						Visible:   false,
					},
					Composite{
						Layout: HBox{MarginsZero: true, Spacing: 8, Alignment: AlignHCenterVNear},
						Children: []Widget{
							PushButton{
								AssignTo: &copyButton,
								Text:     "Copy Code",
								Visible:  false,
								OnClicked: func() {
									code := authManager.DeviceAuthCode()
									if code != nil {
										copyToClipboard(*code)
									}
								},
							},
							PushButton{
								AssignTo: &openBrowserButton,
								Text:     "Open Browser",
								Visible:  false,
								OnClicked: func() {
									url := authManager.DeviceAuthLoginURL()
									if url != nil {
										openBrowser(*url)
									}
								},
							},
						},
					},
					Label{
						AssignTo:  &manualURLLabel,
						Text:      "",
						Alignment: AlignHCenterVNear,
						MaxSize:   Size{Width: 400, Height: 0},
						Visible:   false,
					},
					ProgressBar{
						AssignTo: &progressBar,
						Visible:  false,
					},
					// Success view
					Label{
						AssignTo:  &successLabel,
						Text:      "✓\nAuthentication Successful\nYou have been successfully logged in.",
						Alignment: AlignHCenterVCenter,
						Font:      Font{PointSize: 12, Bold: true},
						Visible:   false,
					},
				},
			},
			VSpacer{},
			// Buttons at bottom
			Composite{
				AssignTo: &buttonComposite,
				Layout:   HBox{MarginsZero: true, Alignment: AlignHFarVNear, Spacing: 8},
				Children: []Widget{
					PushButton{
						AssignTo: &backButton,
						Text:     "Back",
						Visible:  false,
						OnClicked: func() {
							if currentState == stateDeviceAuthCode {
								// Cancel the auth flow
								currentState = stateHostingSelection
								hostingOpt = hostingNone
								hasAutoOpenedBrowser = false
							} else {
								currentState = stateHostingSelection
								hostingOpt = hostingNone
								selfHostedURL = ""
								if urlLineEdit != nil {
									urlLineEdit.SetText("")
								}
							}
							updateUI()
						},
					},
					PushButton{
						AssignTo: &cancelButton,
						Text:     "Cancel",
						OnClicked: func() {
							dlg.Cancel()
						},
					},
					PushButton{
						AssignTo: &loginButton,
						Text:     "Log in",
						Visible:  false,
						OnClicked: func() {
							currentState = stateDeviceAuthCode
							isLoggingIn = true
							updateUI()
							go performLogin()
						},
					},
				},
			},
		},
	}.Create(parent)

	// Disable maximize and minimize buttons
	style := win.GetWindowLong(dlg.Handle(), win.GWL_STYLE)
	style &^= win.WS_MAXIMIZEBOX
	style &^= win.WS_MINIMIZEBOX
	win.SetWindowLong(dlg.Handle(), win.GWL_STYLE, style)

	// Set fixed size
	dlg.SetSize(walk.Size{Width: 450, Height: 400})

	// Load and display word mark logo
	if logoContainer != nil {
		// Determine which word mark to use based on theme
		iconsPath := getIconsPath()
		var imagePath string
		if isDarkMode() {
			imagePath = filepath.Join(iconsPath, "word_mark_white.png")
		} else {
			imagePath = filepath.Join(iconsPath, "word_mark_black.png")
		}

		// Create ImageView widget
		logoImageView, err := walk.NewImageView(logoContainer)
		if err != nil {
			logger.Error("Failed to create ImageView: %v", err)
		} else {
			// Load the image
			img, err := walk.NewImageFromFile(imagePath)
			if err != nil {
				logger.Error("Failed to load word mark image from %s: %v", imagePath, err)
			} else {
				logoImageView.SetImage(img)
			}
		}
	}

	// Update manual URL label
	hostname := configManager.GetHostname()
	if hostname != "" && manualURLLabel != nil {
		manualURL := fmt.Sprintf("If the browser doesn't open, manually visit %s/auth/device-web-auth/start to complete authentication.", hostname)
		manualURLLabel.SetText(manualURL)
	}

	// Initial UI update
	updateUI()

	// Poll for device auth code updates
	go func() {
		for {
			time.Sleep(500 * time.Millisecond)
			if currentState == stateDeviceAuthCode {
				code := authManager.DeviceAuthCode()
				if code != nil {
					updateCodeDisplay()
				} else if !isLoggingIn {
					// Code was cleared, go back
					walk.App().Synchronize(func() {
						currentState = stateHostingSelection
						hostingOpt = hostingNone
						hasAutoOpenedBrowser = false
						updateUI()
					})
				}
			}
		}
	}()

	dlg.Run()
}

// openBrowser opens a URL in the default browser
func openBrowser(url string) {
	cmd := exec.Command("cmd", "/c", "start", url)
	if err := cmd.Run(); err != nil {
		logger.Error("Failed to open browser: %v", err)
	}
}

// copyToClipboard copies text to the Windows clipboard
func copyToClipboard(text string) {
	// Open clipboard
	if !win.OpenClipboard(0) {
		logger.Error("Failed to open clipboard")
		return
	}
	defer win.CloseClipboard()

	// Empty clipboard
	win.EmptyClipboard()

	// Convert text to UTF16
	text16, err := windows.UTF16FromString(text)
	if err != nil {
		logger.Error("Failed to convert text to UTF16: %v", err)
		return
	}

	// Allocate global memory
	memSize := len(text16) * 2
	hMem := win.GlobalAlloc(win.GMEM_MOVEABLE, uintptr(memSize))
	if hMem == 0 {
		logger.Error("Failed to allocate memory")
		return
	}
	defer win.GlobalFree(hMem)

	// Lock memory and copy data
	pMem := win.GlobalLock(hMem)
	if pMem == nil {
		logger.Error("Failed to lock memory")
		return
	}
	defer win.GlobalUnlock(hMem)

	copy((*[1 << 20]uint16)(pMem)[:len(text16):len(text16)], text16)

	// Set clipboard data
	if win.SetClipboardData(win.CF_UNICODETEXT, win.HANDLE(hMem)) == 0 {
		logger.Error("Failed to set clipboard data")
		return
	}
}
