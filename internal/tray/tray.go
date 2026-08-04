//go:build windows

// Package tray is the notification-area UI: live readings, the switches that
// do not need a form, and the entry point into the settings page.
//
// Menu item labels are fixed when the menu is built, and systray offers no way
// to rebuild it, so a language change is applied by relabelling every item on
// the next status update rather than by recreating the menu.
package tray

import (
	"fmt"
	"log/slog"
	"strings"

	"fyne.io/systray"

	"github.com/corgan2222/rig-exporter/internal/app"
	"github.com/corgan2222/rig-exporter/internal/assets"
	"github.com/corgan2222/rig-exporter/internal/collector"
	"github.com/corgan2222/rig-exporter/internal/config"
	"github.com/corgan2222/rig-exporter/internal/i18n"
	"github.com/corgan2222/rig-exporter/internal/metrics"
	"github.com/corgan2222/rig-exporter/internal/winapi"
)

// Options are the bits of context the tray cannot derive from App.
type Options struct {
	// SettingsURL is resolved lazily because the web server may have had to
	// fall back to a different port than the configured one.
	SettingsURL func() string
	LogPath     string
	// OnReady runs after the tray and all menu items are usable. The update
	// helper uses this as the proof that a restarted version really started.
	OnReady func()
	// OnQuit runs after the tray is torn down, before the process exits.
	OnQuit func()
}

// Tray renders the notification-area icon and menu.
type Tray struct {
	app  *app.App
	log  *slog.Logger
	opts Options

	// labelled is the language the fixed menu labels currently carry.
	labelled i18n.Lang
	// header is the top entry's current text, so the address is written only
	// when it has actually changed rather than on every reading.
	header string

	items struct {
		header    *systray.MenuItem
		fps       *systray.MenuItem
		game      *systray.MenuItem
		display   *systray.MenuItem
		load      *systray.MenuItem
		exports   []*systray.MenuItem
		rtss      *systray.MenuItem
		rtssGet   *systray.MenuItem
		pause     *systray.MenuItem
		settings  *systray.MenuItem
		logFile   *systray.MenuItem
		autostart *systray.MenuItem
		quit      *systray.MenuItem
	}
}

// exportSlots is how many export status lines the menu reserves. Menu items
// cannot be inserted after the menu is built, so one slot per possible target
// is created up front and hidden until it is in use.
const exportSlots = 3

// New builds the tray controller.
func New(application *app.App, log *slog.Logger, opts Options) *Tray {
	return &Tray{app: application, log: log, opts: opts}
}

// Run shows the tray icon and blocks until the user quits. It must be called
// from the main goroutine, which is where Windows expects the message loop.
func (t *Tray) Run() {
	systray.Run(t.onReady, t.onExit)
}

// Quit leaves the tray message loop. onExit still owns the shared graceful
// shutdown path, regardless of whether a person or an update requested it.
func (t *Tray) Quit() {
	systray.Quit()
}

func (t *Tray) onReady() {
	systray.SetIcon(assets.Icon)
	systray.SetTitle(config.AppName)
	systray.SetTooltip(config.AppName + " " + config.Version)

	// The name at the top is the obvious thing to click, so it opens the
	// interface rather than sitting there greyed out.
	// Without the address for now — the web server may not have a port yet.
	// renderHeader adds it on the first reading and owns the text from then on.
	t.items.header = systray.AddMenuItem(fmt.Sprintf("%s %s", config.AppName, config.VersionString()), "")
	systray.AddSeparator()

	t.items.fps = addReadout()
	t.items.game = addReadout()
	t.items.display = addReadout()
	t.items.load = addReadout()
	systray.AddSeparator()

	for i := 0; i < exportSlots; i++ {
		item := addReadout()
		item.Hide()
		t.items.exports = append(t.items.exports, item)
	}
	t.items.rtss = addReadout()
	t.items.rtssGet = systray.AddMenuItem("", "")
	t.items.rtssGet.Hide()
	systray.AddSeparator()

	t.items.pause = systray.AddMenuItemCheckbox("", "", t.app.Paused())
	t.items.settings = systray.AddMenuItem("", "")
	t.items.logFile = systray.AddMenuItem("", t.opts.LogPath)
	t.items.autostart = systray.AddMenuItemCheckbox("", "", t.app.Status().Autostart)
	systray.AddSeparator()

	t.items.quit = systray.AddMenuItem("", "")

	t.render(t.app.Status())
	t.app.OnUpdate(t.render)

	go t.handleClicks()
	if t.opts.OnReady != nil {
		t.opts.OnReady()
	}
}

// renderHeader keeps the top entry saying where the interface actually is.
//
// Resolved on every render rather than once when the menu is built: the web
// server may still have been starting then, and it falls back to a random port
// whenever the configured one is taken. A number that is not the one that works
// would be worse than none — this is the entry somebody clicks to get there.
func (t *Tray) renderHeader() {
	text := headerTitle(t.address())
	if text == t.header {
		return
	}
	t.header = text
	t.items.header.SetTitle(text)
}

// headerTitle is the top entry's text. Separated from the menu so it can be
// checked without a notification area.
func headerTitle(address string) string {
	text := fmt.Sprintf("%s %s", config.AppName, config.VersionString())
	if address != "" {
		text += " — " + address
	}
	return text
}

// address is host and port without the scheme: "127.0.0.1:8787" is what a
// person needs out of "http://127.0.0.1:8787", and the menu line is short.
func (t *Tray) address() string {
	if t.opts.SettingsURL == nil {
		return ""
	}
	return strings.TrimPrefix(t.opts.SettingsURL(), "http://")
}

func addReadout() *systray.MenuItem {
	item := systray.AddMenuItem("–", "")
	item.Disable()
	return item
}

func (t *Tray) onExit() {
	if t.opts.OnQuit != nil {
		t.opts.OnQuit()
	}
}

// relabel writes the fixed labels in the given language. It runs once at
// startup and again whenever the language setting changes.
func (t *Tray) relabel(lang i18n.Lang) {
	if t.labelled == lang {
		return
	}
	t.labelled = lang

	t.items.header.SetTooltip(i18n.T(lang, "tray.openInterface"))
	t.items.rtssGet.SetTitle(i18n.T(lang, "tray.rtssDownload"))
	t.items.rtssGet.SetTooltip(i18n.T(lang, "tray.rtssDownloadTip"))
	t.items.pause.SetTitle(i18n.T(lang, "tray.pause"))
	t.items.pause.SetTooltip(i18n.T(lang, "tray.pauseTip"))
	t.items.settings.SetTitle(i18n.T(lang, "tray.settings"))
	t.items.settings.SetTooltip(i18n.T(lang, "tray.settingsTip"))
	t.items.logFile.SetTitle(i18n.T(lang, "tray.log"))
	t.items.autostart.SetTitle(i18n.T(lang, "tray.autostart"))
	t.items.autostart.SetTooltip(i18n.T(lang, "tray.autostartTip"))
	t.items.quit.SetTitle(i18n.T(lang, "tray.quit"))
	t.items.quit.SetTooltip(i18n.T(lang, "tray.quitTip"))
}

// handleClicks owns every menu action. Actions that can fail report through
// the log and a message box, since a tray menu has nowhere to show an error.
func (t *Tray) handleClicks() {
	for {
		select {
		case <-t.items.header.ClickedCh:
			t.open(t.opts.SettingsURL())

		case <-t.items.pause.ClickedCh:
			t.app.SetPaused(!t.app.Paused())

		case <-t.items.settings.ClickedCh:
			t.open(t.opts.SettingsURL())

		case <-t.items.logFile.ClickedCh:
			t.open(t.opts.LogPath)

		case <-t.items.rtssGet.ClickedCh:
			t.open(config.RTSSDownloadURL)

		case <-t.items.autostart.ClickedCh:
			enabled := !t.items.autostart.Checked()
			if err := t.app.SetAutostart(enabled); err != nil {
				lang := t.app.Config().Lang()
				t.log.Error("autostart toggle failed", "error", err)
				winapi.MessageBox(config.AppName,
					i18n.T(lang, "dialog.autostartFailed")+"\n\n"+err.Error(),
					winapi.MBOK|winapi.MBIconWarning)
				continue
			}
			setChecked(t.items.autostart, enabled)

		case <-t.items.quit.ClickedCh:
			systray.Quit()
			return
		}
	}
}

func (t *Tray) open(target string) {
	if target == "" {
		return
	}
	if err := winapi.OpenURL(target); err != nil {
		t.log.Error("open failed", "target", target, "error", err)
	}
}

// render pushes a status update into the menu. It is called from the app's
// polling goroutine, so it only does cheap label updates.
func (t *Tray) render(status app.Status) {
	lang := status.Config.Lang()
	t.relabel(lang)
	t.renderHeader()

	snap := status.Snapshot

	if snap.RTSSStatus.OK() && snap.GameRunning() {
		t.items.fps.SetTitle(fmt.Sprintf("FPS: %.0f  (%.2f ms)", snap.FPS(), snap.FrametimeMs()))
	} else {
		t.items.fps.SetTitle("FPS: –")
	}

	game := snap.Game()
	if game == "" || game == collector.NoGame {
		game = i18n.T(lang, "tray.noGame")
	}
	t.items.game.SetTitle(i18n.T(lang, "tray.game") + ": " + game)

	display := snap.Resolution()
	if hz := snap.RefreshHz(); hz > 0 {
		display = fmt.Sprintf("%s @ %d Hz", display, hz)
	}
	t.items.display.SetTitle(i18n.T(lang, "tray.display") + ": " + display)
	t.items.load.SetTitle(fmt.Sprintf("%s: CPU %.0f %%  ·  RAM %.0f %%%s",
		i18n.T(lang, "tray.load"), snap.CPUPercent(), snap.RAMPercent(), gpuSummary(snap)))

	t.renderExports(status, lang)
	t.items.rtss.SetTitle("RTSS: " + rtssLabel(snap, lang))

	if snap.RTSSStatus.OK() {
		t.items.rtssGet.Hide()
	} else {
		t.items.rtssGet.Show()
	}

	setChecked(t.items.pause, status.Paused)
	setChecked(t.items.autostart, status.Autostart)
	systray.SetTooltip(tooltip(status, lang))
}

// gpuSummary appends the first card's load and temperature to the load line,
// or nothing when no graphics source is available.
func gpuSummary(snap collector.Snapshot) string {
	load, hasLoad := snap.Find(metrics.GPULoad.ID, "0")
	temp, hasTemp := snap.Find(metrics.GPUTemperature.ID, "0")

	switch {
	case hasLoad && hasTemp:
		return fmt.Sprintf("  ·  GPU %.0f %% / %.0f °C", load.Number, temp.Number)
	case hasLoad:
		return fmt.Sprintf("  ·  GPU %.0f %%", load.Number)
	default:
		return ""
	}
}

// renderExports fills the reserved status slots, one per enabled target, and
// hides the rest.
func (t *Tray) renderExports(status app.Status, lang i18n.Lang) {
	for i, item := range t.items.exports {
		if i >= len(status.Exports) {
			item.Hide()
			continue
		}

		e := status.Exports[i]
		marker := "!"
		if e.Healthy {
			marker = "✓"
		}
		item.SetTitle(fmt.Sprintf("%s %s: %s", marker, e.Label, truncate(e.Detail, 70)))
		item.Show()
	}

	if len(status.Exports) == 0 && len(t.items.exports) > 0 {
		t.items.exports[0].SetTitle(i18n.T(lang, "tray.noExport"))
		t.items.exports[0].Show()
	}
}

func rtssLabel(snap collector.Snapshot, lang i18n.Lang) string {
	switch snap.RTSSStatus {
	case collector.RTSSOK:
		if snap.RTSSVersion != "" {
			return i18n.T(lang, "rtss.connected") + " (v" + snap.RTSSVersion + ")"
		}
		return i18n.T(lang, "rtss.connected")
	case collector.RTSSNotRunning:
		return i18n.T(lang, "tray.rtssNotRunning")
	case collector.RTSSAccessDenied:
		return i18n.T(lang, "tray.rtssDenied")
	default:
		return truncate(snap.RTSSMessage, 70)
	}
}

// tooltip is what hovering the tray icon shows. Windows truncates it at 128
// characters, so it stays deliberately short.
func tooltip(status app.Status, lang i18n.Lang) string {
	snap := status.Snapshot

	var b strings.Builder
	b.WriteString(config.AppName)
	if snap.RTSSStatus.OK() && snap.GameRunning() {
		fmt.Fprintf(&b, " · %.0f FPS · %s", snap.FPS(), snap.Game())
	} else if !snap.RTSSStatus.OK() {
		b.WriteString(" · " + i18n.T(lang, "rtss.unavailable"))
	} else {
		b.WriteString(" · " + i18n.T(lang, "tray.noGame"))
	}
	if status.Paused {
		b.WriteString(" · " + i18n.T(lang, "tray.paused"))
	} else if unhealthy := unhealthyExports(status); unhealthy != "" {
		b.WriteString(" · " + unhealthy + " " + i18n.T(lang, "tray.impaired"))
	}
	return truncate(b.String(), 120)
}

// unhealthyExports names the targets that are not currently delivering.
func unhealthyExports(status app.Status) string {
	var broken []string
	for _, e := range status.Exports {
		if !e.Healthy {
			broken = append(broken, e.Label)
		}
	}
	return strings.Join(broken, ", ")
}

func setChecked(item *systray.MenuItem, checked bool) {
	if checked == item.Checked() {
		return
	}
	if checked {
		item.Check()
		return
	}
	item.Uncheck()
}

func truncate(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	if max <= 1 {
		return string(runes[:max])
	}
	return string(runes[:max-1]) + "…"
}
