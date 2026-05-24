//go:build !(linux && 386)

package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/user"
	"runtime"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"
	"fyne.io/systray"
	log "github.com/sirupsen/logrus"

	"github.com/openzro/openzro/client/internal"
	"github.com/openzro/openzro/client/internal/profilemanager"
	"github.com/openzro/openzro/client/proto"
)

// trayProfileRestartHint is appended to the success dialogs of create
// + remove profile when running on a platform where the systray tray
// is known to ignore dynamic submenu add/remove. KDE Plasma's tray
// (via the plasma-workspace StatusNotifierItem plasmoid) caches
// submenu structure after first render and ignores subsequent
// LayoutUpdated signals — so newly-added per-profile items never
// appear in the tray. Verified across fyne.io/systray v1.11.0 and
// v1.12.1. Other Linux DEs (GNOME with AppIndicator, XFCE, Cinnamon,
// MATE) and macOS / Windows render dynamic additions fine.
const trayProfileRestartHint = "Restart the openZro UI to see the updated profile list in the tray menu."

// linuxTrayNeedsRestartForProfileChanges reports whether the local
// systray tray is expected to silently ignore dynamic add/remove of
// profile submenu items. True only when running on KDE Plasma (any
// session type — the bug reproduces on Wayland and X11 alike).
func linuxTrayNeedsRestartForProfileChanges() bool {
	if runtime.GOOS != "linux" {
		return false
	}
	desktop := strings.ToUpper(os.Getenv("XDG_CURRENT_DESKTOP"))
	// XDG_CURRENT_DESKTOP is a colon-separated list per the
	// freedesktop spec; e.g. "KDE", "ubuntu:GNOME", "X-Cinnamon".
	for _, d := range strings.Split(desktop, ":") {
		if d == "KDE" {
			return true
		}
	}
	return false
}

// showProfilesUI creates and displays the Profiles window with a list of existing profiles,
// a button to add new profiles, allows removal, and lets the user switch the active profile.
func (s *serviceClient) showProfilesUI() {

	profiles, err := s.getProfiles()
	if err != nil {
		log.Errorf("get profiles: %v", err)
		return
	}

	var refresh func()
	// List widget for profiles
	list := widget.NewList(
		func() int { return len(profiles) },
		func() fyne.CanvasObject {
			// Each item: Selected indicator, Name, spacer, Select & Remove buttons
			return container.NewHBox(
				widget.NewLabel(""), // indicator
				widget.NewLabel(""), // profile name
				layout.NewSpacer(),
				widget.NewButton("Select", nil),
				widget.NewButton("Remove", nil),
			)
		},
		func(i widget.ListItemID, item fyne.CanvasObject) {
			// Populate each row
			row := item.(*fyne.Container)
			indicator := row.Objects[0].(*widget.Label)
			nameLabel := row.Objects[1].(*widget.Label)
			selectBtn := row.Objects[3].(*widget.Button)
			removeBtn := row.Objects[4].(*widget.Button)

			profile := profiles[i]
			// Show a checkmark if selected
			if profile.IsActive {
				indicator.SetText("✓")
			} else {
				indicator.SetText("")
			}
			nameLabel.SetText(profile.Name)

			// Configure Select/Active button
			selectBtn.SetText(func() string {
				if profile.IsActive {
					return "Active"
				}
				return "Select"
			}())
			selectBtn.OnTapped = func() {
				if profile.IsActive {
					return // already active
				}
				// confirm switch
				dialog.ShowConfirm(
					"Switch Profile",
					fmt.Sprintf("Are you sure you want to switch to '%s'?", profile.Name),
					func(confirm bool) {
						if !confirm {
							return
						}
						// switch
						err = s.switchProfile(profile.Name)
						if err != nil {
							log.Errorf("failed to switch profile: %v", err)
							dialog.ShowError(errors.New("failed to select profile"), s.wProfiles)
							return
						}

						dialog.ShowInformation(
							"Profile Switched",
							fmt.Sprintf("Profile '%s' switched successfully", profile.Name),
							s.wProfiles,
						)

						conn, err := s.getSrvClient(defaultFailTimeout)
						if err != nil {
							log.Errorf("failed to get daemon client: %v", err)
							return
						}

						status, err := conn.Status(context.Background(), &proto.StatusRequest{})
						if err != nil {
							log.Errorf("failed to get status after switching profile: %v", err)
							return
						}

						if status.Status == string(internal.StatusConnected) {
							if err := s.menuDownClick(); err != nil {
								log.Errorf("failed to handle down click after switching profile: %v", err)
								dialog.ShowError(fmt.Errorf("failed to handle down click"), s.wProfiles)
								return
							}
						}
						// update slice flags
						refresh()
					},
					s.wProfiles,
				)
			}

			// Remove profile
			removeBtn.SetText("Remove")
			removeBtn.OnTapped = func() {
				dialog.ShowConfirm(
					"Delete Profile",
					fmt.Sprintf("Are you sure you want to delete '%s'?", profile.Name),
					func(confirm bool) {
						if !confirm {
							return
						}
						// remove
						err = s.removeProfile(profile.Name)
						if err != nil {
							log.Errorf("failed to remove profile: %v", err)
							dialog.ShowError(fmt.Errorf("failed to remove profile"), s.wProfiles)
							return
						}
						msg := fmt.Sprintf("Profile '%s' removed successfully.", profile.Name)
						if linuxTrayNeedsRestartForProfileChanges() {
							msg += "\n\n" + trayProfileRestartHint
						}
						dialog.ShowInformation(
							"Profile Removed",
							msg,
							s.wProfiles,
						)
						// update slice
						refresh()
					},
					s.wProfiles,
				)
			}
		},
	)

	refresh = func() {
		newProfiles, err := s.getProfiles()
		if err != nil {
			dialog.ShowError(err, s.wProfiles)
			return
		}
		profiles = newProfiles // update the slice
		list.Refresh()         // tell Fyne to re-call length/update on every visible row
	}

	// Button to add a new profile
	newBtn := widget.NewButton("New Profile", func() {
		nameEntry := widget.NewEntry()
		nameEntry.SetPlaceHolder("Enter Profile Name")

		formItems := []*widget.FormItem{{Text: "Name:", Widget: nameEntry}}
		dlg := dialog.NewForm(
			"New Profile",
			"Create",
			"Cancel",
			formItems,
			func(confirm bool) {
				if !confirm {
					return
				}
				name := nameEntry.Text
				if name == "" {
					dialog.ShowError(errors.New("profile name cannot be empty"), s.wProfiles)
					return
				}

				// add profile
				err = s.addProfile(name)
				if err != nil {
					log.Errorf("failed to create profile: %v", err)
					dialog.ShowError(fmt.Errorf("failed to create profile"), s.wProfiles)
					return
				}
				msg := fmt.Sprintf("Profile '%s' created successfully.", name)
				if linuxTrayNeedsRestartForProfileChanges() {
					msg += "\n\n" + trayProfileRestartHint
				}
				dialog.ShowInformation(
					"Profile Created",
					msg,
					s.wProfiles,
				)
				// update slice
				refresh()
			},
			s.wProfiles,
		)
		// make dialog wider
		dlg.Resize(fyne.NewSize(350, 150))
		dlg.Show()
	})

	// Assemble window content
	content := container.NewBorder(nil, newBtn, nil, nil, list)
	s.wProfiles = s.app.NewWindow("Openzro Profiles")
	s.wProfiles.SetContent(content)
	s.wProfiles.Resize(fyne.NewSize(400, 300))
	s.wProfiles.SetOnClosed(s.cancel)

	s.wProfiles.Show()
}

func (s *serviceClient) addProfile(profileName string) error {
	conn, err := s.getSrvClient(defaultFailTimeout)
	if err != nil {
		return fmt.Errorf(getClientFMT, err)
	}

	currUser, err := user.Current()
	if err != nil {
		return fmt.Errorf("get current user: %w", err)
	}

	_, err = conn.AddProfile(context.Background(), &proto.AddProfileRequest{
		ProfileName: profileName,
		Username:    currUser.Username,
	})

	if err != nil {
		return fmt.Errorf("add profile: %w", err)
	}

	return nil
}

func (s *serviceClient) switchProfile(profileName string) error {
	conn, err := s.getSrvClient(defaultFailTimeout)
	if err != nil {
		return fmt.Errorf(getClientFMT, err)
	}

	currUser, err := user.Current()
	if err != nil {
		return fmt.Errorf("get current user: %w", err)
	}

	if _, err := conn.SwitchProfile(context.Background(), &proto.SwitchProfileRequest{
		ProfileName: &profileName,
		Username:    &currUser.Username,
	}); err != nil {
		return fmt.Errorf("switch profile failed: %w", err)
	}

	err = s.profileManager.SwitchProfile(profileName)
	if err != nil {
		return fmt.Errorf("switch profile: %w", err)
	}

	return nil
}

func (s *serviceClient) removeProfile(profileName string) error {
	conn, err := s.getSrvClient(defaultFailTimeout)
	if err != nil {
		return fmt.Errorf(getClientFMT, err)
	}

	currUser, err := user.Current()
	if err != nil {
		return fmt.Errorf("get current user: %w", err)
	}

	_, err = conn.RemoveProfile(context.Background(), &proto.RemoveProfileRequest{
		ProfileName: profileName,
		Username:    currUser.Username,
	})
	if err != nil {
		return fmt.Errorf("remove profile: %w", err)
	}

	return nil
}

type Profile struct {
	Name     string
	IsActive bool
}

func (s *serviceClient) getProfiles() ([]Profile, error) {
	conn, err := s.getSrvClient(defaultFailTimeout)
	if err != nil {
		return nil, fmt.Errorf(getClientFMT, err)
	}

	currUser, err := user.Current()
	if err != nil {
		return nil, fmt.Errorf("get current user: %w", err)
	}
	profilesResp, err := conn.ListProfiles(context.Background(), &proto.ListProfilesRequest{
		Username: currUser.Username,
	})
	if err != nil {
		return nil, fmt.Errorf("list profiles: %w", err)
	}

	var profiles []Profile

	for _, profile := range profilesResp.Profiles {
		profiles = append(profiles, Profile{
			Name:     profile.Name,
			IsActive: profile.IsActive,
		})
	}

	return profiles, nil
}

type subItem struct {
	*systray.MenuItem
	ctx    context.Context
	cancel context.CancelFunc
	// name lets refresh() identify whether an existing item matches a
	// profile in the freshly-loaded list. Stable items survive
	// refreshes — destroying + recreating systray subitems corrupts
	// the internal menu-item ID table on Linux/GTK ("systray error:
	// no menu item with ID N") and kills subsequent clicks.
	name string
}

type profileMenu struct {
	mu                    sync.Mutex
	ctx                   context.Context
	profileManager        *profilemanager.ProfileManager
	eventHandler          *eventHandler
	profileMenuItem       *systray.MenuItem
	emailMenuItem         *systray.MenuItem
	profileSubItems       []*subItem
	manageProfilesSubItem *subItem
	profilesState         []Profile
	downClickCallback     func() error
	upClickCallback       func() error
	getSrvClientCallback  func(timeout time.Duration) (proto.DaemonServiceClient, error)
	loadSettingsCallback  func()
	app                   fyne.App
}

type newProfileMenuArgs struct {
	ctx                  context.Context
	profileManager       *profilemanager.ProfileManager
	eventHandler         *eventHandler
	profileMenuItem      *systray.MenuItem
	emailMenuItem        *systray.MenuItem
	downClickCallback    func() error
	upClickCallback      func() error
	getSrvClientCallback func(timeout time.Duration) (proto.DaemonServiceClient, error)
	loadSettingsCallback func()
	app                  fyne.App
}

func newProfileMenu(args newProfileMenuArgs) *profileMenu {
	p := profileMenu{
		ctx:                  args.ctx,
		profileManager:       args.profileManager,
		eventHandler:         args.eventHandler,
		profileMenuItem:      args.profileMenuItem,
		emailMenuItem:        args.emailMenuItem,
		downClickCallback:    args.downClickCallback,
		upClickCallback:      args.upClickCallback,
		getSrvClientCallback: args.getSrvClientCallback,
		loadSettingsCallback: args.loadSettingsCallback,
		app:                  args.app,
	}

	p.emailMenuItem.Disable()
	p.emailMenuItem.Hide()
	// Create the "Manage Profiles" subitem ONCE. Stable across refreshes
	// — refresh() never touches it. Avoids the destroy/recreate cycle
	// that corrupts the systray's internal menu-item ID table on
	// Linux/GTK ("systray error: no menu item with ID N") and kills
	// subsequent clicks on the item.
	p.setupManageProfilesItem()
	// First refresh populates the per-profile submenu items from the
	// current daemon-side list. On macOS/Windows + KDE X11 + most
	// other DEs, subsequent refreshes also add/remove items
	// dynamically. On KDE Plasma Wayland the dbusmenu tray caches
	// submenu structure after first render — dynamic adds/removes go
	// unrendered. We document the limitation in the Manage Profiles
	// window dialogs (Linux only) so operators know to restart the UI
	// to see new profiles in the tray.
	p.refresh()
	go p.updateMenu()

	return &p
}

// setupManageProfilesItem creates the "Manage Profiles" submenu item
// once and wires its click handler. The item is stable across
// refresh() calls — its lifetime matches the profileMenu's.
func (p *profileMenu) setupManageProfilesItem() {
	ctx, cancel := context.WithCancel(context.Background())
	manageItem := p.profileMenuItem.AddSubMenuItem("Manage Profiles", "")
	p.manageProfilesSubItem = &subItem{MenuItem: manageItem, ctx: ctx, cancel: cancel, name: "Manage Profiles"}

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case _, ok := <-manageItem.ClickedCh:
				if !ok {
					return
				}
				// Spawn the profile-management window subprocess. After it
				// exits, refresh the per-profile sub-items (a new profile
				// may have been created) and let the settings page pick up
				// any changes. p.refresh() no longer touches this item.
				p.eventHandler.runSelfCommand(p.ctx, "profiles", "true")
				p.refresh()
				p.loadSettingsCallback()
			}
		}
	}()
}

// spawnProfileClickHandler launches the per-item click goroutine for
// a per-profile subItem. The item's IsActive state is re-checked at
// click time (against p.profilesState) — captured IsActive would go
// stale across refreshes. Lives until si.ctx is cancelled (i.e. the
// profile got removed and refresh swept the item).
func (p *profileMenu) spawnProfileClickHandler(si *subItem, profileName, username string) {
	go func() {
		for {
			select {
			case <-si.ctx.Done():
				return
			case _, ok := <-si.ClickedCh:
				if !ok {
					return
				}
				p.mu.Lock()
				alreadyActive := false
				for _, ps := range p.profilesState {
					if ps.Name == profileName && ps.IsActive {
						alreadyActive = true
						break
					}
				}
				p.mu.Unlock()
				if alreadyActive {
					log.Infof("Profile '%s' is already active", profileName)
					continue
				}
				conn, err := p.getSrvClientCallback(defaultFailTimeout)
				if err != nil {
					log.Errorf("failed to get daemon client: %v", err)
					continue
				}
				name := profileName
				usr := username
				if _, err := conn.SwitchProfile(si.ctx, &proto.SwitchProfileRequest{
					ProfileName: &name,
					Username:    &usr,
				}); err != nil {
					log.Errorf("failed to switch profile: %v", err)
					p.app.SendNotification(fyne.NewNotification("Error", "Failed to switch profile"))
					continue
				}
				if err := p.profileManager.SwitchProfile(profileName); err != nil {
					log.Errorf("failed to switch profile '%s': %v", profileName, err)
					continue
				}
				log.Infof("Switched to profile '%s'", profileName)

				status, err := conn.Status(si.ctx, &proto.StatusRequest{})
				if err != nil {
					log.Errorf("failed to get status after switching profile: %v", err)
					continue
				}
				if status.Status == string(internal.StatusConnected) {
					if err := p.downClickCallback(); err != nil {
						log.Errorf("failed to handle down click after switching profile: %v", err)
					}
				}
				if err := p.upClickCallback(); err != nil {
					log.Errorf("failed to handle up click after switching profile: %v", err)
				}
				p.refresh()
				p.loadSettingsCallback()
			}
		}
	}()
}

func (p *profileMenu) getProfiles() ([]Profile, error) {
	conn, err := p.getSrvClientCallback(defaultFailTimeout)
	if err != nil {
		return nil, fmt.Errorf(getClientFMT, err)
	}
	currUser, err := user.Current()
	if err != nil {
		return nil, fmt.Errorf("get current user: %w", err)
	}

	profilesResp, err := conn.ListProfiles(p.ctx, &proto.ListProfilesRequest{
		Username: currUser.Username,
	})
	if err != nil {
		return nil, fmt.Errorf("list profiles: %w", err)
	}

	var profiles []Profile

	for _, profile := range profilesResp.Profiles {
		profiles = append(profiles, Profile{
			Name:     profile.Name,
			IsActive: profile.IsActive,
		})
	}

	return profiles, nil
}

func (p *profileMenu) refresh() {
	p.mu.Lock()
	defer p.mu.Unlock()

	profiles, err := p.getProfiles()
	if err != nil {
		log.Errorf("failed to list profiles: %v", err)
		return
	}

	currUser, err := user.Current()
	if err != nil {
		log.Errorf("failed to get current user: %v", err)
		return
	}

	conn, err := p.getSrvClientCallback(defaultFailTimeout)
	if err != nil {
		log.Errorf("failed to get daemon client: %v", err)
		return
	}

	activeProf, err := conn.GetActiveProfile(p.ctx, &proto.GetActiveProfileRequest{})
	if err != nil {
		log.Errorf("failed to get active profile: %v", err)
		return
	}

	if activeProf.ProfileName == "default" || activeProf.Username == currUser.Username {
		activeProfState, err := p.profileManager.GetProfileState(activeProf.ProfileName)
		if err != nil {
			log.Warnf("failed to get active profile state: %v", err)
			p.emailMenuItem.Hide()
		} else if activeProfState.Email != "" {
			p.emailMenuItem.SetTitle(fmt.Sprintf("(%s)", activeProfState.Email))
			p.emailMenuItem.Show()
		}
	}

	// Diff-aware reconciliation of per-profile submenu items:
	//   - profile still present → reuse the existing menu item (just
	//     toggle Check). NEVER destroy+recreate stable items, otherwise
	//     the systray ID table corrupts on Linux/GTK ("systray error:
	//     no menu item with ID N") and clicks die.
	//   - profile gone         → Remove + cancel its click handler.
	//   - new profile          → AddSubMenuItem + spawn click handler.
	//     On macOS/Windows + most Linux DEs this renders immediately.
	//     On KDE Plasma Wayland the new item exists internally but the
	//     tray panel ignores LayoutUpdated for new children — the
	//     management window's Linux-only dialog tells the operator to
	//     restart the UI.
	existingByName := make(map[string]*subItem, len(p.profileSubItems))
	for _, si := range p.profileSubItems {
		existingByName[si.name] = si
	}
	keep := make(map[string]struct{}, len(profiles))
	newItems := make([]*subItem, 0, len(profiles))
	for _, profile := range profiles {
		keep[profile.Name] = struct{}{}
		if reused, ok := existingByName[profile.Name]; ok {
			if profile.IsActive {
				reused.Check()
			} else {
				reused.Uncheck()
			}
			newItems = append(newItems, reused)
			continue
		}
		item := p.profileMenuItem.AddSubMenuItem(profile.Name, "")
		if profile.IsActive {
			item.Check()
		}
		ctx, cancel := context.WithCancel(context.Background())
		si := &subItem{MenuItem: item, ctx: ctx, cancel: cancel, name: profile.Name}
		newItems = append(newItems, si)
		p.spawnProfileClickHandler(si, profile.Name, currUser.Username)
	}
	for _, old := range p.profileSubItems {
		if _, kept := keep[old.name]; !kept {
			old.Remove()
			old.cancel()
		}
	}
	p.profileSubItems = newItems
	p.profilesState = profiles

	if activeProf.ProfileName == "default" || activeProf.Username == currUser.Username {
		p.profileMenuItem.SetTitle(activeProf.ProfileName)
	} else {
		p.profileMenuItem.SetTitle(fmt.Sprintf("Profile: %s (User: %s)", activeProf.ProfileName, activeProf.Username))
		p.emailMenuItem.Hide()
	}
}

func (p *profileMenu) updateMenu() {
	// check every second
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:

			// get profilesList
			profiles, err := p.getProfiles()
			if err != nil {
				log.Errorf("failed to list profiles: %v", err)
				continue
			}

			sort.Slice(profiles, func(i, j int) bool {
				return profiles[i].Name < profiles[j].Name
			})

			p.mu.Lock()
			state := p.profilesState
			p.mu.Unlock()

			sort.Slice(state, func(i, j int) bool {
				return state[i].Name < state[j].Name
			})

			if slices.Equal(profiles, state) {
				continue
			}

			p.refresh()
		case <-p.ctx.Done():
			return // context canceled

		}
	}
}
