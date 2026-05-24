//go:build !(linux && 386)

package main

import (
	"context"
	_ "embed"
	"errors"
	"flag"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"os/user"
	"path"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"fyne.io/systray"
	"github.com/cenkalti/backoff/v4"
	log "github.com/sirupsen/logrus"
	"github.com/skratchdot/open-golang/open"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/openzro/openzro/client/iface"
	"github.com/openzro/openzro/client/internal"
	"github.com/openzro/openzro/client/internal/profilemanager"
	"github.com/openzro/openzro/client/proto"
	"github.com/openzro/openzro/client/ui/desktop"
	"github.com/openzro/openzro/client/ui/event"
	"github.com/openzro/openzro/client/ui/process"

	"github.com/openzro/openzro/util"

	"github.com/openzro/openzro/version"
)

const (
	defaultFailTimeout = 3 * time.Second
	failFastTimeout    = time.Second
)

const (
	censoredPreSharedKey = "**********"
)

func main() {
	flags := parseFlags()

	// Initialize file logging if needed.
	var logFile string
	if flags.saveLogsInFile {
		file, err := initLogFile()
		if err != nil {
			log.Errorf("error while initializing log: %v", err)
			return
		}
		logFile = file
	} else {
		_ = util.InitLog("trace", util.LogConsole)
	}

	// Create the Fyne application.
	a := app.NewWithID("Openzro")
	a.SetIcon(fyne.NewStaticResource("openzro", iconDisconnected))

	// Show error message window if needed.
	if flags.errorMsg != "" {
		showErrorMessage(flags.errorMsg)
		return
	}

	// Create the service client (this also builds the settings or networks UI if requested).
	client := newServiceClient(&newServiceClientArgs{
		addr:         flags.daemonAddr,
		logFile:      logFile,
		app:          a,
		showSettings: flags.showSettings,
		showNetworks: flags.showNetworks,
		showLoginURL: flags.showLoginURL,
		showDebug:    flags.showDebug,
		showProfiles: flags.showProfiles,
	})

	// Watch for theme/settings changes to update the icon.
	go watchSettingsChanges(a, client)

	// Run in window mode if any UI flag was set.
	if flags.showSettings || flags.showNetworks || flags.showDebug || flags.showLoginURL || flags.showProfiles {
		a.Run()
		return
	}

	// Check for another running process.
	pid, running, err := process.IsAnotherProcessRunning()
	if err != nil {
		log.Errorf("error while checking process: %v", err)
		return
	}
	if running {
		log.Warnf("another process is running with pid %d, exiting", pid)
		return
	}

	client.setDefaultFonts()
	systray.Run(client.onTrayReady, client.onTrayExit)
}

type cliFlags struct {
	daemonAddr     string
	showSettings   bool
	showNetworks   bool
	showProfiles   bool
	showDebug      bool
	showLoginURL   bool
	errorMsg       string
	saveLogsInFile bool
}

// parseFlags reads and returns all needed command-line flags.
func parseFlags() *cliFlags {
	var flags cliFlags

	defaultDaemonAddr := "unix:///var/run/openzro.sock"
	if runtime.GOOS == "windows" {
		// 41831, NOT NetBird's 41731 — must match the daemon default
		// in client/cmd/root.go so the UI dials the openZro daemon
		// (and both can coexist with a NetBird install on Windows).
		defaultDaemonAddr = "tcp://127.0.0.1:41831"
	}
	flag.StringVar(&flags.daemonAddr, "daemon-addr", defaultDaemonAddr, "Daemon service address to serve CLI requests [unix|tcp]://[path|host:port]")
	flag.BoolVar(&flags.showSettings, "settings", false, "run settings window")
	flag.BoolVar(&flags.showNetworks, "networks", false, "run networks window")
	flag.BoolVar(&flags.showProfiles, "profiles", false, "run profiles window")
	flag.BoolVar(&flags.showDebug, "debug", false, "run debug window")
	flag.StringVar(&flags.errorMsg, "error-msg", "", "displays an error message window")
	flag.BoolVar(&flags.saveLogsInFile, "use-log-file", false, fmt.Sprintf("save logs in a file: %s/openzro-ui-PID.log", os.TempDir()))
	flag.BoolVar(&flags.showLoginURL, "login-url", false, "show login URL in a popup window")
	flag.Parse()
	return &flags
}

// initLogFile initializes logging into a file.
func initLogFile() (string, error) {
	logFile := path.Join(os.TempDir(), fmt.Sprintf("openzro-ui-%d.log", os.Getpid()))
	return logFile, util.InitLog("trace", logFile)
}

// watchSettingsChanges listens for Fyne theme/settings changes and updates the client icon.
func watchSettingsChanges(a fyne.App, client *serviceClient) {
	settingsChangeChan := make(chan fyne.Settings)
	a.Settings().AddChangeListener(settingsChangeChan)
	for range settingsChangeChan {
		client.updateIcon()
	}
}

// showErrorMessage displays an error message in a simple window.
func showErrorMessage(msg string) {
	a := app.New()
	w := a.NewWindow("Openzro Error")
	label := widget.NewLabel(msg)
	label.Wrapping = fyne.TextWrapWord
	w.SetContent(label)
	w.Resize(fyne.NewSize(400, 100))
	w.Show()
	a.Run()
}

//go:embed assets/openzro-systemtray-connected-macos.png
var iconConnectedMacOS []byte

//go:embed assets/openzro-systemtray-disconnected-macos.png
var iconDisconnectedMacOS []byte

//go:embed assets/openzro-systemtray-update-disconnected-macos.png
var iconUpdateDisconnectedMacOS []byte

//go:embed assets/openzro-systemtray-update-connected-macos.png
var iconUpdateConnectedMacOS []byte

//go:embed assets/openzro-systemtray-connecting-macos.png
var iconConnectingMacOS []byte

//go:embed assets/openzro-systemtray-error-macos.png
var iconErrorMacOS []byte

//go:embed assets/connected.png
var iconConnectedDot []byte

//go:embed assets/disconnected.png
var iconDisconnectedDot []byte

type serviceClient struct {
	ctx    context.Context
	cancel context.CancelFunc
	addr   string
	conn   proto.DaemonServiceClient

	eventHandler *eventHandler

	profileManager *profilemanager.ProfileManager

	icAbout              []byte
	icConnected          []byte
	icConnectedDot       []byte
	icDisconnected       []byte
	icDisconnectedDot    []byte
	icUpdateConnected    []byte
	icUpdateDisconnected []byte
	icConnecting         []byte
	icError              []byte

	// systray menu items
	mStatus            *systray.MenuItem
	mUp                *systray.MenuItem
	mDown              *systray.MenuItem
	mSettings          *systray.MenuItem
	mProfile           *profileMenu
	mAbout             *systray.MenuItem
	mGitHub            *systray.MenuItem
	mVersionUI         *systray.MenuItem
	mVersionDaemon     *systray.MenuItem
	mUpdateStatus      *systray.MenuItem
	mInstallUpdate     *systray.MenuItem
	mDownloadUpdate    *systray.MenuItem
	mQuit              *systray.MenuItem
	mNetworks          *systray.MenuItem
	mAllowSSH          *systray.MenuItem
	mAutoConnect       *systray.MenuItem
	mEnableRosenpass   *systray.MenuItem
	mLazyConnEnabled   *systray.MenuItem
	mBlockInbound      *systray.MenuItem
	mNotifications     *systray.MenuItem
	mAdvancedSettings  *systray.MenuItem
	mCreateDebugBundle *systray.MenuItem
	mExitNode          *systray.MenuItem

	// application with main windows.
	app                  fyne.App
	wSettings            fyne.Window
	showAdvancedSettings bool
	sendNotification     bool

	// input elements for settings form
	iMngURL        *widget.Entry
	iLogFile       *widget.Entry
	iPreSharedKey  *widget.Entry
	iInterfaceName *widget.Entry
	iInterfacePort *widget.Entry

	// switch elements for settings form
	sRosenpassPermissive *widget.Check
	sNetworkMonitor      *widget.Check
	sDisableDNS          *widget.Check
	sDisableClientRoutes *widget.Check
	sDisableServerRoutes *widget.Check
	sBlockLANAccess      *widget.Check

	// observable settings over corresponding iMngURL and iPreSharedKey values.
	managementURL       string
	preSharedKey        string
	RosenpassPermissive bool
	interfaceName       string
	interfacePort       int
	networkMonitor      bool
	disableDNS          bool
	disableClientRoutes bool
	disableServerRoutes bool
	blockLANAccess      bool

	connected            bool
	daemonVersion        string
	updateIndicationLock sync.Mutex
	isUpdateIconActive   bool
	showNetworks         bool
	wNetworks            fyne.Window
	wProfiles            fyne.Window

	// trayCache memoises the last value applied to each systray
	// setter that updateStatus / setConnectingStatus /
	// setDisconnectedStatus / applyUpdateStateLocked would otherwise
	// re-fire on every 2s tick. Each helper that pushes a property
	// out to fyne.io/systray checks this cache first and skips the
	// call when the value is unchanged. Eliminates the repaint-per-
	// tick that GTK/X11 + KDE Plasma render as visible flicker
	// (steals the user's hover/selection mid-menu).
	trayCache trayStateCache

	eventManager *event.Manager

	exitNodeMu           sync.Mutex
	mExitNodeItems       []menuHandler
	exitNodeStates       []exitNodeState
	mExitNodeDeselectAll *systray.MenuItem
	logFile              string
	wLoginURL            fyne.Window
}

// trayStateCache holds the last-applied value for every systray
// property that updateStatus and friends would otherwise unconditionally
// re-set on every poll tick. `initialized` flips to true after the
// first apply — before that, every field is treated as "needs apply"
// regardless of whether the desired value happens to equal the zero
// value (avoids the "first Enable() never fires because !want==false"
// trap). mExitTouched does the same job specifically for mExit, which
// is driven by updateExitNodes outside the connected/connecting/
// disconnected switch and therefore may be set before applyTray runs.
//
// mu is independent from serviceClient.updateIndicationLock because
// setDisconnectedStatus is called both from inside the lock (normal
// tick path) and from outside (the RPC-failed branch at the top of
// updateStatus). A dedicated short-held mutex keeps the cache
// thread-safe in both call sites.
type trayStateCache struct {
	mu            sync.Mutex
	initialized   bool
	iconKey       string
	tooltip       string
	statusTitle   string
	statusIconKey string
	mUpDisabled   bool
	mDownDisabled bool
	mNetDisabled  bool
	mExitDisabled bool
	mExitTouched  bool
	updateItems   map[string]updateItemState
}

// updateItemState memoises one row of the About-submenu update group.
// applyUpdateMenuItem re-fires SetTitle/SetTooltip/Show/Hide on three
// items every 2s; without caching that's six SetTitle + three Hide/Show
// per tick on a typical session.
type updateItemState struct {
	shown   bool
	title   string
	tooltip string
}

// desiredTrayState is the full set of properties one branch of
// updateStatus wants to push to the tray. nil-able fields opt the
// helper out of touching a given property (used for mExit, which is
// driven by updateExitNodes outside the connected/connecting/
// disconnected switch).
type desiredTrayState struct {
	iconKey         string
	iconBytes       []byte
	iconMacOSBytes  []byte
	tooltip         string
	statusTitle     string
	statusIconKey   string // empty → don't touch
	statusIconBytes []byte
	mUpDisabled     bool
	mDownDisabled   bool
	mNetDisabled    bool
	mExitDisabled   *bool // nil → leave alone
}

type menuHandler struct {
	*systray.MenuItem
	cancel context.CancelFunc
}

type newServiceClientArgs struct {
	addr         string
	logFile      string
	app          fyne.App
	showSettings bool
	showNetworks bool
	showDebug    bool
	showLoginURL bool
	showProfiles bool
}

// newServiceClient instance constructor
//
// This constructor also builds the UI elements for the settings window.
func newServiceClient(args *newServiceClientArgs) *serviceClient {
	ctx, cancel := context.WithCancel(context.Background())
	s := &serviceClient{
		ctx:              ctx,
		cancel:           cancel,
		addr:             args.addr,
		app:              args.app,
		logFile:          args.logFile,
		sendNotification: false,

		showAdvancedSettings: args.showSettings,
		showNetworks:         args.showNetworks,
	}

	s.eventHandler = newEventHandler(s)
	s.profileManager = profilemanager.NewProfileManager()
	s.setNewIcons()

	switch {
	case args.showSettings:
		s.showSettingsUI()
	case args.showNetworks:
		s.showNetworksUI()
	case args.showLoginURL:
		s.showLoginURL()
	case args.showDebug:
		s.showDebugUI()
	case args.showProfiles:
		s.showProfilesUI()
	}

	return s
}

func (s *serviceClient) setNewIcons() {
	s.icAbout = iconAbout
	s.icConnectedDot = iconConnectedDot
	s.icDisconnectedDot = iconDisconnectedDot
	if s.app.Settings().ThemeVariant() == theme.VariantDark {
		s.icConnected = iconConnectedDark
		s.icDisconnected = iconDisconnected
		s.icUpdateConnected = iconUpdateConnectedDark
		s.icUpdateDisconnected = iconUpdateDisconnectedDark
		s.icConnecting = iconConnectingDark
		s.icError = iconErrorDark
	} else {
		s.icConnected = iconConnected
		s.icDisconnected = iconDisconnected
		s.icUpdateConnected = iconUpdateConnected
		s.icUpdateDisconnected = iconUpdateDisconnected
		s.icConnecting = iconConnecting
		s.icError = iconError
	}
}

func (s *serviceClient) updateIcon() {
	s.setNewIcons()
	s.updateIndicationLock.Lock()
	if s.connected {
		if s.isUpdateIconActive {
			systray.SetTemplateIcon(iconUpdateConnectedMacOS, s.icUpdateConnected)
		} else {
			systray.SetTemplateIcon(iconConnectedMacOS, s.icConnected)
		}
	} else {
		if s.isUpdateIconActive {
			systray.SetTemplateIcon(iconUpdateDisconnectedMacOS, s.icUpdateDisconnected)
		} else {
			systray.SetTemplateIcon(iconDisconnectedMacOS, s.icDisconnected)
		}
	}
	s.updateIndicationLock.Unlock()
}

func (s *serviceClient) showSettingsUI() {
	// add settings window UI elements.
	s.wSettings = s.app.NewWindow("Openzro Settings")
	s.wSettings.SetOnClosed(s.cancel)

	s.iMngURL = widget.NewEntry()

	s.iLogFile = widget.NewEntry()
	s.iLogFile.Disable()
	s.iPreSharedKey = widget.NewPasswordEntry()
	s.iInterfaceName = widget.NewEntry()
	s.iInterfacePort = widget.NewEntry()

	s.sRosenpassPermissive = widget.NewCheck("Enable Rosenpass permissive mode", nil)

	s.sNetworkMonitor = widget.NewCheck("Restarts Openzro when the network changes", nil)
	s.sDisableDNS = widget.NewCheck("Keeps system DNS settings unchanged", nil)
	s.sDisableClientRoutes = widget.NewCheck("This peer won't route traffic to other peers", nil)
	s.sDisableServerRoutes = widget.NewCheck("This peer won't act as router for others", nil)
	s.sBlockLANAccess = widget.NewCheck("Blocks local network access when used as exit node", nil)

	s.wSettings.SetContent(s.getSettingsForm())
	s.wSettings.Resize(fyne.NewSize(600, 500))
	s.wSettings.SetFixedSize(true)

	s.getSrvConfig()
	s.wSettings.Show()
}

// getSettingsForm to embed it into settings window.
func (s *serviceClient) getSettingsForm() *widget.Form {

	var activeProfName string
	activeProf, err := s.profileManager.GetActiveProfile()
	if err != nil {
		log.Errorf("get active profile: %v", err)
	} else {
		activeProfName = activeProf.Name
	}
	return &widget.Form{
		Items: []*widget.FormItem{
			{Text: "Profile", Widget: widget.NewLabel(activeProfName)},
			{Text: "Quantum-Resistance", Widget: s.sRosenpassPermissive},
			{Text: "Interface Name", Widget: s.iInterfaceName},
			{Text: "Interface Port", Widget: s.iInterfacePort},
			{Text: "Management URL", Widget: s.iMngURL},
			{Text: "Pre-shared Key", Widget: s.iPreSharedKey},
			{Text: "Log File", Widget: s.iLogFile},
			{Text: "Network Monitor", Widget: s.sNetworkMonitor},
			{Text: "Disable DNS", Widget: s.sDisableDNS},
			{Text: "Disable Client Routes", Widget: s.sDisableClientRoutes},
			{Text: "Disable Server Routes", Widget: s.sDisableServerRoutes},
			{Text: "Disable LAN Access", Widget: s.sBlockLANAccess},
		},
		SubmitText: "Save",
		OnSubmit: func() {
			if s.iPreSharedKey.Text != "" && s.iPreSharedKey.Text != censoredPreSharedKey {
				// validate preSharedKey if it added
				if _, err := wgtypes.ParseKey(s.iPreSharedKey.Text); err != nil {
					dialog.ShowError(fmt.Errorf("Invalid Pre-shared Key Value"), s.wSettings)
					return
				}
			}

			port, err := strconv.ParseInt(s.iInterfacePort.Text, 10, 64)
			if err != nil {
				dialog.ShowError(errors.New("Invalid interface port"), s.wSettings)
				return
			}

			iMngURL := strings.TrimSpace(s.iMngURL.Text)

			defer s.wSettings.Close()

			// Check if any settings have changed
			if s.managementURL != iMngURL || s.preSharedKey != s.iPreSharedKey.Text ||
				s.RosenpassPermissive != s.sRosenpassPermissive.Checked ||
				s.interfaceName != s.iInterfaceName.Text || s.interfacePort != int(port) ||
				s.networkMonitor != s.sNetworkMonitor.Checked ||
				s.disableDNS != s.sDisableDNS.Checked ||
				s.disableClientRoutes != s.sDisableClientRoutes.Checked ||
				s.disableServerRoutes != s.sDisableServerRoutes.Checked ||
				s.blockLANAccess != s.sBlockLANAccess.Checked {

				s.managementURL = iMngURL
				s.preSharedKey = s.iPreSharedKey.Text

				currUser, err := user.Current()
				if err != nil {
					log.Errorf("get current user: %v", err)
					return
				}

				var req proto.SetConfigRequest
				req.ProfileName = activeProf.Name
				req.Username = currUser.Username

				if iMngURL != "" {
					req.ManagementUrl = iMngURL
				}

				req.RosenpassPermissive = &s.sRosenpassPermissive.Checked
				req.InterfaceName = &s.iInterfaceName.Text
				req.WireguardPort = &port
				req.NetworkMonitor = &s.sNetworkMonitor.Checked
				req.DisableDns = &s.sDisableDNS.Checked
				req.DisableClientRoutes = &s.sDisableClientRoutes.Checked
				req.DisableServerRoutes = &s.sDisableServerRoutes.Checked
				req.BlockLanAccess = &s.sBlockLANAccess.Checked

				if s.iPreSharedKey.Text != censoredPreSharedKey {
					req.OptionalPreSharedKey = &s.iPreSharedKey.Text
				}

				conn, err := s.getSrvClient(failFastTimeout)
				if err != nil {
					log.Errorf("get client: %v", err)
					dialog.ShowError(fmt.Errorf("Failed to connect to the service: %v", err), s.wSettings)
					return
				}
				_, err = conn.SetConfig(s.ctx, &req)
				if err != nil {
					log.Errorf("set config: %v", err)
					dialog.ShowError(fmt.Errorf("Failed to set configuration: %v", err), s.wSettings)
					return
				}

				status, err := conn.Status(s.ctx, &proto.StatusRequest{})
				if err != nil {
					log.Errorf("get service status: %v", err)
					dialog.ShowError(fmt.Errorf("Failed to get service status: %v", err), s.wSettings)
					return
				}
				if status.Status == string(internal.StatusConnected) {
					// run down & up
					_, err = conn.Down(s.ctx, &proto.DownRequest{})
					if err != nil {
						log.Errorf("down service: %v", err)
					}

					_, err = conn.Up(s.ctx, &proto.UpRequest{})
					if err != nil {
						log.Errorf("up service: %v", err)
						dialog.ShowError(fmt.Errorf("Failed to reconnect: %v", err), s.wSettings)
						return
					}
				}

			}
		},
		OnCancel: func() {
			s.wSettings.Close()
		},
	}
}

func (s *serviceClient) login(openURL bool) (*proto.LoginResponse, error) {
	conn, err := s.getSrvClient(defaultFailTimeout)
	if err != nil {
		log.Errorf("get client: %v", err)
		return nil, err
	}

	activeProf, err := s.profileManager.GetActiveProfile()
	if err != nil {
		log.Errorf("get active profile: %v", err)
		return nil, err
	}

	currUser, err := user.Current()
	if err != nil {
		return nil, fmt.Errorf("get current user: %w", err)
	}

	loginResp, err := conn.Login(s.ctx, &proto.LoginRequest{
		IsUnixDesktopClient: runtime.GOOS == "linux" || runtime.GOOS == "freebsd",
		ProfileName:         &activeProf.Name,
		Username:            &currUser.Username,
	})
	if err != nil {
		log.Errorf("login to management URL with: %v", err)
		return nil, err
	}

	if loginResp.NeedsSSOLogin && openURL {
		err = s.handleSSOLogin(loginResp, conn)
		if err != nil {
			log.Errorf("handle SSO login failed: %v", err)
			return nil, err
		}
	}

	return loginResp, nil
}

func (s *serviceClient) handleSSOLogin(loginResp *proto.LoginResponse, conn proto.DaemonServiceClient) error {
	err := open.Run(loginResp.VerificationURIComplete)
	if err != nil {
		log.Errorf("opening the verification uri in the browser failed: %v", err)
		return err
	}

	resp, err := conn.WaitSSOLogin(s.ctx, &proto.WaitSSOLoginRequest{UserCode: loginResp.UserCode})
	if err != nil {
		log.Errorf("waiting sso login failed with: %v", err)
		return err
	}

	if resp.Email != "" {
		err := s.profileManager.SetActiveProfileState(&profilemanager.ProfileState{
			Email: resp.Email,
		})
		if err != nil {
			log.Warnf("failed to set profile state: %v", err)
		} else {
			s.mProfile.refresh()
		}

	}

	return nil
}

func (s *serviceClient) menuUpClick() error {
	systray.SetTemplateIcon(iconConnectingMacOS, s.icConnecting)
	conn, err := s.getSrvClient(defaultFailTimeout)
	if err != nil {
		systray.SetTemplateIcon(iconErrorMacOS, s.icError)
		log.Errorf("get client: %v", err)
		return err
	}

	_, err = s.login(true)
	if err != nil {
		log.Errorf("login failed with: %v", err)
		return err
	}

	status, err := conn.Status(s.ctx, &proto.StatusRequest{})
	if err != nil {
		log.Errorf("get service status: %v", err)
		return err
	}

	if status.Status == string(internal.StatusConnected) {
		log.Warnf("already connected")
		return nil
	}

	if _, err := s.conn.Up(s.ctx, &proto.UpRequest{}); err != nil {
		log.Errorf("up service: %v", err)
		return err
	}

	return nil
}

func (s *serviceClient) menuDownClick() error {
	systray.SetTemplateIcon(iconConnectingMacOS, s.icConnecting)
	conn, err := s.getSrvClient(defaultFailTimeout)
	if err != nil {
		log.Errorf("get client: %v", err)
		return err
	}

	status, err := conn.Status(s.ctx, &proto.StatusRequest{})
	if err != nil {
		log.Errorf("get service status: %v", err)
		return err
	}

	if status.Status != string(internal.StatusConnected) && status.Status != string(internal.StatusConnecting) {
		log.Warnf("already down")
		return nil
	}

	if _, err := s.conn.Down(s.ctx, &proto.DownRequest{}); err != nil {
		log.Errorf("down service: %v", err)
		return err
	}

	return nil
}

func (s *serviceClient) updateStatus() error {
	conn, err := s.getSrvClient(defaultFailTimeout)
	if err != nil {
		return err
	}
	err = backoff.Retry(func() error {
		status, err := conn.Status(s.ctx, &proto.StatusRequest{})
		if err != nil {
			log.Errorf("get service status: %v", err)
			if s.connected {
				s.app.SendNotification(fyne.NewNotification("Error", "Connection to service lost"))
			}
			s.setDisconnectedStatus()
			return err
		}

		s.updateIndicationLock.Lock()
		defer s.updateIndicationLock.Unlock()

		// notify the user when the session has expired
		if status.Status == string(internal.StatusSessionExpired) {
			s.onSessionExpire()
		}

		// openZro #5: the daemon (driven by the management Sync
		// directive) is the single source of truth for update
		// availability. Apply it before the connected-state switch so
		// the icon branch below reads a fresh isUpdateIconActive.
		s.applyUpdateStateLocked(status.GetUpdateState())

		switch {
		case status.Status == string(internal.StatusConnected):
			s.connected = true
			s.sendNotification = true
			iconKey, iconBytes, iconMacOS := s.connectedIcon()
			s.applyTray(desiredTrayState{
				iconKey:         iconKey,
				iconBytes:       iconBytes,
				iconMacOSBytes:  iconMacOS,
				tooltip:         "Openzro (Connected)",
				statusTitle:     "Connected",
				statusIconKey:   "connected-dot",
				statusIconBytes: s.icConnectedDot,
				mUpDisabled:     true,
				mDownDisabled:   false,
				mNetDisabled:    false,
				// mExit driven by updateExitNodes goroutine below.
			})
			go s.updateExitNodes()
		case status.Status == string(internal.StatusConnecting):
			s.setConnectingStatus()
		case status.Status != string(internal.StatusConnected) && s.mUp.Disabled():
			s.setDisconnectedStatus()
		}

		// Daemon version changed (e.g. after a self-update restart):
		// refresh only the "Daemon: x.y.z" menu line. Update
		// availability and the tray icon are owned by
		// applyUpdateStateLocked / the connected-state switch above
		// (openZro #5 — the daemon, not a UI-side GitHub poll, is the
		// source of truth).
		if s.daemonVersion != status.DaemonVersion {
			s.daemonVersion = status.DaemonVersion

			daemonVersionTitle := normalizedVersion(s.daemonVersion)
			s.mVersionDaemon.SetTitle(fmt.Sprintf("Daemon: %s", daemonVersionTitle))
			s.mVersionDaemon.SetTooltip(fmt.Sprintf("Daemon version: %s", daemonVersionTitle))
			s.mVersionDaemon.Show()
		}

		return nil
	}, &backoff.ExponentialBackOff{
		InitialInterval:     time.Second,
		RandomizationFactor: backoff.DefaultRandomizationFactor,
		Multiplier:          backoff.DefaultMultiplier,
		MaxInterval:         300 * time.Millisecond,
		MaxElapsedTime:      2 * time.Second,
		Stop:                backoff.Stop,
		Clock:               backoff.SystemClock,
	})
	if err != nil {
		return err
	}

	return nil
}

func (s *serviceClient) setDisconnectedStatus() {
	s.connected = false
	exitDisabled := true
	iconKey, iconBytes, iconMacOS := s.disconnectedIcon()
	s.applyTray(desiredTrayState{
		iconKey:         iconKey,
		iconBytes:       iconBytes,
		iconMacOSBytes:  iconMacOS,
		tooltip:         "Openzro (Disconnected)",
		statusTitle:     "Disconnected",
		statusIconKey:   "disconnected-dot",
		statusIconBytes: s.icDisconnectedDot,
		mUpDisabled:     false,
		mDownDisabled:   true,
		mNetDisabled:    true,
		mExitDisabled:   &exitDisabled,
	})
	go s.updateExitNodes()
}

func (s *serviceClient) setConnectingStatus() {
	s.connected = false
	exitDisabled := true
	s.applyTray(desiredTrayState{
		iconKey:        "connecting",
		iconBytes:      s.icConnecting,
		iconMacOSBytes: iconConnectingMacOS,
		tooltip:        "Openzro (Connecting)",
		statusTitle:    "Connecting",
		// statusIconKey left empty → don't touch mStatus.SetIcon.
		// The pre-refactor code never set the dot icon on
		// Connecting either, so behavior is preserved.
		mUpDisabled:   true,
		mDownDisabled: false,
		mNetDisabled:  true,
		mExitDisabled: &exitDisabled,
	})
}

// connectedIcon returns the cache key + byte slices for the connected
// icon, branching on whether an update is currently available.
func (s *serviceClient) connectedIcon() (string, []byte, []byte) {
	if s.isUpdateIconActive {
		return "connected-update", s.icUpdateConnected, iconUpdateConnectedMacOS
	}
	return "connected", s.icConnected, iconConnectedMacOS
}

// disconnectedIcon returns the cache key + byte slices for the
// disconnected icon, branching on whether an update is currently
// available.
func (s *serviceClient) disconnectedIcon() (string, []byte, []byte) {
	if s.isUpdateIconActive {
		return "disconnected-update", s.icUpdateDisconnected, iconUpdateDisconnectedMacOS
	}
	return "disconnected", s.icDisconnected, iconDisconnectedMacOS
}

// applyTray pushes `desired` to the systray, skipping any setter
// whose value matches the last-applied entry in s.trayCache. Safe to
// call from any goroutine; the cache has its own mutex independent
// of serviceClient.updateIndicationLock.
func (s *serviceClient) applyTray(d desiredTrayState) {
	s.trayCache.mu.Lock()
	defer s.trayCache.mu.Unlock()

	first := !s.trayCache.initialized

	if first || s.trayCache.iconKey != d.iconKey {
		systray.SetTemplateIcon(d.iconMacOSBytes, d.iconBytes)
		s.trayCache.iconKey = d.iconKey
	}
	if first || s.trayCache.tooltip != d.tooltip {
		systray.SetTooltip(d.tooltip)
		s.trayCache.tooltip = d.tooltip
	}
	if first || s.trayCache.statusTitle != d.statusTitle {
		s.mStatus.SetTitle(d.statusTitle)
		s.trayCache.statusTitle = d.statusTitle
	}
	if d.statusIconKey != "" && (first || s.trayCache.statusIconKey != d.statusIconKey) {
		s.mStatus.SetIcon(d.statusIconBytes)
		s.trayCache.statusIconKey = d.statusIconKey
	}
	if first || s.trayCache.mUpDisabled != d.mUpDisabled {
		setMenuItemEnabled(s.mUp, !d.mUpDisabled)
		s.trayCache.mUpDisabled = d.mUpDisabled
	}
	if first || s.trayCache.mDownDisabled != d.mDownDisabled {
		setMenuItemEnabled(s.mDown, !d.mDownDisabled)
		s.trayCache.mDownDisabled = d.mDownDisabled
	}
	if first || s.trayCache.mNetDisabled != d.mNetDisabled {
		setMenuItemEnabled(s.mNetworks, !d.mNetDisabled)
		s.trayCache.mNetDisabled = d.mNetDisabled
	}
	if d.mExitDisabled != nil {
		want := *d.mExitDisabled
		if !s.trayCache.mExitTouched || s.trayCache.mExitDisabled != want {
			setMenuItemEnabled(s.mExitNode, !want)
			s.trayCache.mExitDisabled = want
			s.trayCache.mExitTouched = true
		}
	}

	s.trayCache.initialized = true
}

// setMenuItemEnabled is a tiny wrapper that maps the bool we cache
// (enabled? true/false) to the two distinct systray.MenuItem methods.
func setMenuItemEnabled(item *systray.MenuItem, enabled bool) {
	if enabled {
		item.Enable()
	} else {
		item.Disable()
	}
}

// setExitNodeEnabledCached toggles mExitNode via the shared trayCache
// so the goroutine spawned by updateExitNodes (re-fired every 2s
// during a connected session) only pushes Enable/Disable when the
// desired state actually flipped. Without the cache the unconditional
// Enable/Disable at the end of updateExitNodes repaints the menu on
// every tick and steals the user's mouse hover.
func (s *serviceClient) setExitNodeEnabledCached(enabled bool) {
	s.trayCache.mu.Lock()
	defer s.trayCache.mu.Unlock()
	want := !enabled
	if !s.trayCache.mExitTouched || s.trayCache.mExitDisabled != want {
		setMenuItemEnabled(s.mExitNode, enabled)
		s.trayCache.mExitDisabled = want
		s.trayCache.mExitTouched = true
	}
}

func (s *serviceClient) onTrayReady() {
	systray.SetTemplateIcon(iconDisconnectedMacOS, s.icDisconnected)
	systray.SetTooltip("Openzro")

	// setup systray menu items
	s.mStatus = systray.AddMenuItem("Disconnected", "Disconnected")
	s.mStatus.SetIcon(s.icDisconnectedDot)
	s.mStatus.Disable()

	profileMenuItem := systray.AddMenuItem("", "")
	emailMenuItem := systray.AddMenuItem("", "")

	newProfileMenuArgs := &newProfileMenuArgs{
		ctx:                  s.ctx,
		profileManager:       s.profileManager,
		eventHandler:         s.eventHandler,
		profileMenuItem:      profileMenuItem,
		emailMenuItem:        emailMenuItem,
		downClickCallback:    s.menuDownClick,
		upClickCallback:      s.menuUpClick,
		getSrvClientCallback: s.getSrvClient,
		loadSettingsCallback: s.loadSettings,
		app:                  s.app,
	}

	s.mProfile = newProfileMenu(*newProfileMenuArgs)

	systray.AddSeparator()
	s.mUp = systray.AddMenuItem("Connect", "Connect")
	s.mDown = systray.AddMenuItem("Disconnect", "Disconnect")
	s.mDown.Disable()
	systray.AddSeparator()

	s.mSettings = systray.AddMenuItem("Settings", settingsMenuDescr)
	s.mAllowSSH = s.mSettings.AddSubMenuItemCheckbox("Allow SSH", allowSSHMenuDescr, false)
	s.mAutoConnect = s.mSettings.AddSubMenuItemCheckbox("Connect on Startup", autoConnectMenuDescr, false)
	s.mEnableRosenpass = s.mSettings.AddSubMenuItemCheckbox("Enable Quantum-Resistance", quantumResistanceMenuDescr, false)
	s.mLazyConnEnabled = s.mSettings.AddSubMenuItemCheckbox("Enable Lazy Connections", lazyConnMenuDescr, false)
	s.mBlockInbound = s.mSettings.AddSubMenuItemCheckbox("Block Inbound Connections", blockInboundMenuDescr, false)
	s.mNotifications = s.mSettings.AddSubMenuItemCheckbox("Notifications", notificationsMenuDescr, false)
	s.mAdvancedSettings = s.mSettings.AddSubMenuItem("Advanced Settings", advancedSettingsMenuDescr)
	s.mCreateDebugBundle = s.mSettings.AddSubMenuItem("Create Debug Bundle", debugBundleMenuDescr)
	s.loadSettings()

	s.exitNodeMu.Lock()
	s.mExitNode = systray.AddMenuItem("Exit Node", exitNodeMenuDescr)
	s.mExitNode.Disable()
	s.exitNodeMu.Unlock()

	s.mNetworks = systray.AddMenuItem("Networks", networksMenuDescr)
	s.mNetworks.Disable()
	systray.AddSeparator()

	s.mAbout = systray.AddMenuItem("About", "About")
	s.mAbout.SetIcon(s.icAbout)

	s.mGitHub = s.mAbout.AddSubMenuItem("GitHub", "GitHub")

	versionString := normalizedVersion(version.OpenzroVersion())
	s.mVersionUI = s.mAbout.AddSubMenuItem(fmt.Sprintf("GUI: %s", versionString), fmt.Sprintf("GUI Version: %s", versionString))
	s.mVersionUI.Disable()

	s.mVersionDaemon = s.mAbout.AddSubMenuItem("", "")
	s.mVersionDaemon.Disable()
	s.mVersionDaemon.Hide()

	// #5 R5: a non-actionable status line. Visible whenever there is
	// an active management directive (available, forced/silent, or
	// "checking…") so the user sees WHAT version and WHY even when no
	// manual CTA is offered. Replaces the retired poll-era "Download
	// latest version" browser link — in the management-driven model a
	// manual browser download would bypass the gated/verified pipeline.
	s.mUpdateStatus = s.mAbout.AddSubMenuItem("", "")
	s.mUpdateStatus.Disable()
	s.mUpdateStatus.Hide()

	// #5: ask the privileged daemon to download+verify+install the
	// update (rollout-gated). Only shown for a non-force directive
	// (forced installs are silent); label carries the target version.
	// macOS-only — Windows/Linux can't auto-install (no signed pkg
	// pipeline, package-manager landscape respectively).
	s.mInstallUpdate = s.mAbout.AddSubMenuItem("Install update now", "Download, verify and install the update via the daemon")
	s.mInstallUpdate.Hide()

	// Windows-only counterpart: a Download CTA that opens the direct
	// MSI asset URL in the default browser. Linux has no CTA — the
	// tray icon badge is the entire signal (apt/dnf/pacman handle
	// install). Hidden on init; applyUpdateStateLocked decides.
	s.mDownloadUpdate = s.mAbout.AddSubMenuItem("Download update", "Open the MSI installer download page")
	s.mDownloadUpdate.Hide()

	systray.AddSeparator()
	s.mQuit = systray.AddMenuItem("Quit", quitMenuDescr)

	// update exit node menu in case service is already connected
	go s.updateExitNodes()

	go func() {
		s.getSrvConfig()
		time.Sleep(100 * time.Millisecond) // To prevent race condition caused by systray not being fully initialized and ignoring setIcon
		for {
			err := s.updateStatus()
			if err != nil {
				log.Errorf("error while updating status: %v", err)
			}
			time.Sleep(2 * time.Second)
		}
	}()

	s.eventManager = event.NewManager(s.app, s.addr)
	s.eventManager.SetNotificationsEnabled(s.mNotifications.Checked())
	s.eventManager.AddHandler(func(event *proto.SystemEvent) {
		if event.Category == proto.SystemEvent_SYSTEM {
			s.updateExitNodes()
		}
	})

	go s.eventManager.Start(s.ctx)
	go s.eventHandler.listen(s.ctx)
}

func (s *serviceClient) attachOutput(cmd *exec.Cmd) *os.File {
	if s.logFile == "" {
		// attach child's streams to parent's streams
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr

		return nil
	}

	out, err := os.OpenFile(s.logFile, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		log.Errorf("Failed to open log file %s: %v", s.logFile, err)
		return nil
	}
	cmd.Stdout = out
	cmd.Stderr = out
	return out
}

func normalizedVersion(version string) string {
	versionString := version
	if unicode.IsDigit(rune(versionString[0])) {
		versionString = fmt.Sprintf("v%s", versionString)
	}
	return versionString
}

// onTrayExit is called when the tray icon is closed.
func (s *serviceClient) onTrayExit() {
	s.cancel()
}

// getSrvClient connection to the service.
func (s *serviceClient) getSrvClient(timeout time.Duration) (proto.DaemonServiceClient, error) {
	if s.conn != nil {
		return s.conn, nil
	}

	ctx, cancel := context.WithTimeout(s.ctx, timeout)
	defer cancel()

	conn, err := grpc.DialContext(
		ctx,
		strings.TrimPrefix(s.addr, "tcp://"),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
		grpc.WithUserAgent(desktop.GetUIUserAgent()),
	)
	if err != nil {
		return nil, fmt.Errorf("dial service: %w", err)
	}

	s.conn = proto.NewDaemonServiceClient(conn)
	return s.conn, nil
}

// getSrvConfig from the service to show it in the settings window.
func (s *serviceClient) getSrvConfig() {
	s.managementURL = profilemanager.DefaultManagementURL

	_, err := s.profileManager.GetActiveProfile()
	if err != nil {
		log.Errorf("get active profile: %v", err)
		return
	}

	var cfg *profilemanager.Config

	conn, err := s.getSrvClient(failFastTimeout)
	if err != nil {
		log.Errorf("get client: %v", err)
		return
	}

	currUser, err := user.Current()
	if err != nil {
		log.Errorf("get current user: %v", err)
		return
	}

	activeProf, err := s.profileManager.GetActiveProfile()
	if err != nil {
		log.Errorf("get active profile: %v", err)
		return
	}

	srvCfg, err := conn.GetConfig(s.ctx, &proto.GetConfigRequest{
		ProfileName: activeProf.Name,
		Username:    currUser.Username,
	})
	if err != nil {
		log.Errorf("get config settings from server: %v", err)
		return
	}

	cfg = protoConfigToConfig(srvCfg)

	if cfg.ManagementURL.String() != "" {
		s.managementURL = cfg.ManagementURL.String()
	}
	s.preSharedKey = cfg.PreSharedKey
	s.RosenpassPermissive = cfg.RosenpassPermissive
	s.interfaceName = cfg.WgIface
	s.interfacePort = cfg.WgPort

	s.networkMonitor = *cfg.NetworkMonitor
	s.disableDNS = cfg.DisableDNS
	s.disableClientRoutes = cfg.DisableClientRoutes
	s.disableServerRoutes = cfg.DisableServerRoutes
	s.blockLANAccess = cfg.BlockLANAccess

	if s.showAdvancedSettings {
		s.iMngURL.SetText(s.managementURL)
		s.iPreSharedKey.SetText(cfg.PreSharedKey)
		s.iInterfaceName.SetText(cfg.WgIface)
		s.iInterfacePort.SetText(strconv.Itoa(cfg.WgPort))
		s.sRosenpassPermissive.SetChecked(cfg.RosenpassPermissive)
		if !cfg.RosenpassEnabled {
			s.sRosenpassPermissive.Disable()
		}
		s.sNetworkMonitor.SetChecked(*cfg.NetworkMonitor)
		s.sDisableDNS.SetChecked(cfg.DisableDNS)
		s.sDisableClientRoutes.SetChecked(cfg.DisableClientRoutes)
		s.sDisableServerRoutes.SetChecked(cfg.DisableServerRoutes)
		s.sBlockLANAccess.SetChecked(cfg.BlockLANAccess)
	}

	if s.mNotifications == nil {
		return
	}
	if cfg.DisableNotifications != nil && *cfg.DisableNotifications {
		s.mNotifications.Uncheck()
	} else {
		s.mNotifications.Check()
	}
	if s.eventManager != nil {
		s.eventManager.SetNotificationsEnabled(s.mNotifications.Checked())
	}
}

func protoConfigToConfig(cfg *proto.GetConfigResponse) *profilemanager.Config {

	var config profilemanager.Config

	if cfg.ManagementUrl != "" {
		parsed, err := url.Parse(cfg.ManagementUrl)
		if err != nil {
			log.Errorf("parse management URL: %v", err)
		} else {
			config.ManagementURL = parsed
		}
	}

	if cfg.PreSharedKey != "" {
		if cfg.PreSharedKey != censoredPreSharedKey {
			config.PreSharedKey = cfg.PreSharedKey
		} else {
			config.PreSharedKey = ""
		}
	}
	if cfg.AdminURL != "" {
		parsed, err := url.Parse(cfg.AdminURL)
		if err != nil {
			log.Errorf("parse admin URL: %v", err)
		} else {
			config.AdminURL = parsed
		}
	}

	config.WgIface = cfg.InterfaceName
	if cfg.WireguardPort != 0 {
		config.WgPort = int(cfg.WireguardPort)
	} else {
		config.WgPort = iface.DefaultWgPort
	}

	config.DisableAutoConnect = cfg.DisableAutoConnect
	config.ServerSSHAllowed = &cfg.ServerSSHAllowed
	config.RosenpassEnabled = cfg.RosenpassEnabled
	config.RosenpassPermissive = cfg.RosenpassPermissive
	config.DisableNotifications = &cfg.DisableNotifications
	config.LazyConnectionEnabled = cfg.LazyConnectionEnabled
	config.BlockInbound = cfg.BlockInbound
	config.NetworkMonitor = &cfg.NetworkMonitor
	config.DisableDNS = cfg.DisableDns
	config.DisableClientRoutes = cfg.DisableClientRoutes
	config.DisableServerRoutes = cfg.DisableServerRoutes
	config.BlockLANAccess = cfg.BlockLanAccess

	return &config
}

// applyUpdateStateLocked reflects the daemon's management-driven
// self-update verdict (openZro #5) into the tray menu. The caller
// holds updateIndicationLock and runs the connected/disconnected
// icon switch right after, which already branches on
// isUpdateIconActive — so this only flips the flag and the menu
// items, no icon call here (avoids a double-set fighting the switch).
// us may be nil (no directive / post-restart): the getters are
// nil-safe and yield the not-available, menus-hidden state.
//
// Decision is delegated to decideUpdateMenu (pure, testable); this
// method is the thin glue that turns the verdict into Show/Hide/
// SetTitle calls on the opaque systray items.
func (s *serviceClient) applyUpdateStateLocked(us *proto.UpdateState) {
	s.isUpdateIconActive = us.GetAvailable()
	v := decideUpdateMenu(runtime.GOOS, us)
	s.applyUpdateMenuItemCached("status", s.mUpdateStatus, v.statusShown, v.statusTitle, v.statusTooltip)
	s.applyUpdateMenuItemCached("install", s.mInstallUpdate, v.installShown, v.installTitle, v.installTooltip)
	s.applyUpdateMenuItemCached("download", s.mDownloadUpdate, v.downloadShown, v.downloadTitle, v.downloadTooltip)
}

// applyUpdateMenuItemCached delegates to applyUpdateMenuItem only when
// the desired (shown/title/tooltip) tuple differs from the last value
// pushed to that item. Stops the About-submenu update group from
// flickering on every 2s poll tick (3 items × Hide/Show + SetTitle +
// SetTooltip per tick = a lot of repaint noise on GTK/X11).
func (s *serviceClient) applyUpdateMenuItemCached(key string, item *systray.MenuItem, show bool, title, tooltip string) {
	s.trayCache.mu.Lock()
	defer s.trayCache.mu.Unlock()

	if s.trayCache.updateItems == nil {
		s.trayCache.updateItems = make(map[string]updateItemState, 3)
	}
	last, seen := s.trayCache.updateItems[key]
	want := updateItemState{shown: show, title: title, tooltip: tooltip}
	if seen && last == want {
		return
	}
	applyUpdateMenuItem(item, show, title, tooltip)
	s.trayCache.updateItems[key] = want
}

// updateMenuVerdict is the per-platform decision about how the
// update items in the About submenu should render. Pure data so
// the platform branching is testable without a live systray.
type updateMenuVerdict struct {
	statusShown   bool
	statusTitle   string
	statusTooltip string

	installShown   bool
	installTitle   string
	installTooltip string

	downloadShown   bool
	downloadTitle   string
	downloadTooltip string
}

// decideUpdateMenu maps (goos, daemon update state) → which items
// in the About submenu show, with what labels and tooltips:
//
//   - macOS: status line + "Install openZro X" CTA wired to the
//     daemon Update RPC. Force directives hide the CTA (the daemon
//     installs silently).
//   - Windows: status line + "Download openZro X (.msi)" CTA that
//     opens the MSI asset URL in the browser. No silent install on
//     Windows — info-only by design.
//   - Linux + others: badge-only. The package manager (apt/dnf/
//     pacman/…) is the install path; a tray CTA would be redundant.
//
// us may be nil; all getters are nil-safe and yield the menus-hidden
// "no directive yet" verdict.
func decideUpdateMenu(goos string, us *proto.UpdateState) updateMenuVerdict {
	target := us.GetTargetVersion()
	decision := us.GetLastDecision()
	available := us.GetAvailable()
	force := us.GetForce()

	var v updateMenuVerdict
	switch goos {
	case "darwin":
		if target != "" {
			v.statusShown = true
			v.statusTitle = "Update: " + target
			v.statusTooltip = decision
		}
		if available && !force {
			v.installShown = true
			if target != "" {
				v.installTitle = "Install openZro " + target
			} else {
				v.installTitle = "Install update now"
			}
			v.installTooltip = decision
		}
	case "windows":
		if target != "" {
			v.statusShown = true
			v.statusTitle = "Update: " + target
			v.statusTooltip = decision
		}
		if available && target != "" {
			v.downloadShown = true
			v.downloadTitle = "Download openZro " + target + " (.msi)"
			v.downloadTooltip = decision
		}
	}
	return v
}

// applyUpdateMenuItem is the opaque-systray glue: SetTitle + optional
// SetTooltip + Show when shown, Hide otherwise. Kept tiny so the
// pure decideUpdateMenu carries the policy and this stays mechanical.
func applyUpdateMenuItem(item *systray.MenuItem, show bool, title, tooltip string) {
	if !show {
		item.Hide()
		return
	}
	item.SetTitle(title)
	if tooltip != "" {
		item.SetTooltip(tooltip)
	}
	item.Show()
}

// onSessionExpire sends a notification to the user when the session expires.
func (s *serviceClient) onSessionExpire() {
	s.sendNotification = true
	if s.sendNotification {
		go s.eventHandler.runSelfCommand(s.ctx, "login-url", "true")
		s.sendNotification = false
	}
}

// loadSettings loads the settings from the config file and updates the UI elements accordingly.
func (s *serviceClient) loadSettings() {
	conn, err := s.getSrvClient(failFastTimeout)
	if err != nil {
		log.Errorf("get client: %v", err)
		return
	}

	currUser, err := user.Current()
	if err != nil {
		log.Errorf("get current user: %v", err)
		return
	}

	activeProf, err := s.profileManager.GetActiveProfile()
	if err != nil {
		log.Errorf("get active profile: %v", err)
		return
	}

	cfg, err := conn.GetConfig(s.ctx, &proto.GetConfigRequest{
		ProfileName: activeProf.Name,
		Username:    currUser.Username,
	})
	if err != nil {
		log.Errorf("get config settings from server: %v", err)
		return
	}

	if cfg.ServerSSHAllowed {
		s.mAllowSSH.Check()
	} else {
		s.mAllowSSH.Uncheck()
	}

	if cfg.DisableAutoConnect {
		s.mAutoConnect.Uncheck()
	} else {
		s.mAutoConnect.Check()
	}

	if cfg.RosenpassEnabled {
		s.mEnableRosenpass.Check()
	} else {
		s.mEnableRosenpass.Uncheck()
	}

	if cfg.LazyConnectionEnabled {
		s.mLazyConnEnabled.Check()
	} else {
		s.mLazyConnEnabled.Uncheck()
	}

	if cfg.BlockInbound {
		s.mBlockInbound.Check()
	} else {
		s.mBlockInbound.Uncheck()
	}

	if cfg.DisableNotifications {
		s.mNotifications.Uncheck()
	} else {
		s.mNotifications.Check()
	}
	if s.eventManager != nil {
		s.eventManager.SetNotificationsEnabled(s.mNotifications.Checked())
	}
}

// updateConfig updates the configuration parameters
// based on the values selected in the settings window.
func (s *serviceClient) updateConfig() error {
	disableAutoStart := !s.mAutoConnect.Checked()
	sshAllowed := s.mAllowSSH.Checked()
	rosenpassEnabled := s.mEnableRosenpass.Checked()
	lazyConnectionEnabled := s.mLazyConnEnabled.Checked()
	blockInbound := s.mBlockInbound.Checked()
	notificationsDisabled := !s.mNotifications.Checked()

	activeProf, err := s.profileManager.GetActiveProfile()
	if err != nil {
		log.Errorf("get active profile: %v", err)
		return err
	}

	currUser, err := user.Current()
	if err != nil {
		log.Errorf("get current user: %v", err)
		return err
	}

	conn, err := s.getSrvClient(failFastTimeout)
	if err != nil {
		log.Errorf("get client: %v", err)
		return err
	}

	req := proto.SetConfigRequest{
		ProfileName:           activeProf.Name,
		Username:              currUser.Username,
		DisableAutoConnect:    &disableAutoStart,
		ServerSSHAllowed:      &sshAllowed,
		RosenpassEnabled:      &rosenpassEnabled,
		LazyConnectionEnabled: &lazyConnectionEnabled,
		BlockInbound:          &blockInbound,
		DisableNotifications:  &notificationsDisabled,
	}

	if _, err := conn.SetConfig(s.ctx, &req); err != nil {
		log.Errorf("set config settings on server: %v", err)
		return err
	}

	return nil
}

// showLoginURL creates a borderless window styled like a pop-up in the top-right corner using s.wLoginURL.
func (s *serviceClient) showLoginURL() {

	resIcon := fyne.NewStaticResource("openzro.png", iconAbout)

	if s.wLoginURL == nil {
		s.wLoginURL = s.app.NewWindow("Openzro Session Expired")
		s.wLoginURL.Resize(fyne.NewSize(400, 200))
		s.wLoginURL.SetIcon(resIcon)
	}
	// add a description label
	label := widget.NewLabel("Your Openzro session has expired.\nPlease re-authenticate to continue using Openzro.")

	btn := widget.NewButtonWithIcon("Re-authenticate", theme.ViewRefreshIcon(), func() {

		conn, err := s.getSrvClient(defaultFailTimeout)
		if err != nil {
			log.Errorf("get client: %v", err)
			return
		}

		resp, err := s.login(false)
		if err != nil {
			log.Errorf("failed to fetch login URL: %v", err)
			return
		}
		verificationURL := resp.VerificationURIComplete
		if verificationURL == "" {
			verificationURL = resp.VerificationURI
		}

		if verificationURL == "" {
			log.Error("no verification URL provided in the login response")
			return
		}

		if err := openURL(verificationURL); err != nil {
			log.Errorf("failed to open login URL: %v", err)
			return
		}

		_, err = conn.WaitSSOLogin(s.ctx, &proto.WaitSSOLoginRequest{UserCode: resp.UserCode})
		if err != nil {
			log.Errorf("Waiting sso login failed with: %v", err)
			label.SetText("Waiting login failed, please create \na debug bundle in the settings and contact support.")
			return
		}

		label.SetText("Re-authentication successful.\nReconnecting")
		status, err := conn.Status(s.ctx, &proto.StatusRequest{})
		if err != nil {
			log.Errorf("get service status: %v", err)
			return
		}

		if status.Status == string(internal.StatusConnected) {
			label.SetText("Already connected.\nClosing this window.")
			time.Sleep(2 * time.Second)
			s.wLoginURL.Close()
			return
		}

		_, err = conn.Up(s.ctx, &proto.UpRequest{})
		if err != nil {
			label.SetText("Reconnecting failed, please create \na debug bundle in the settings and contact support.")
			log.Errorf("Reconnecting failed with: %v", err)
			return
		}

		label.SetText("Connection successful.\nClosing this window.")
		time.Sleep(time.Second)

		s.wLoginURL.Close()
	})

	img := canvas.NewImageFromResource(resIcon)
	img.FillMode = canvas.ImageFillContain
	img.SetMinSize(fyne.NewSize(64, 64))
	img.Resize(fyne.NewSize(64, 64))

	// center the content vertically
	content := container.NewVBox(
		layout.NewSpacer(),
		img,
		label,
		btn,
		layout.NewSpacer(),
	)
	s.wLoginURL.SetContent(container.NewCenter(content))

	s.wLoginURL.Show()
}

func openURL(url string) error {
	var err error
	switch runtime.GOOS {
	case "windows":
		err = exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	case "darwin":
		err = exec.Command("open", url).Start()
	case "linux", "freebsd":
		err = exec.Command("xdg-open", url).Start()
	default:
		err = fmt.Errorf("unsupported platform")
	}
	return err
}
