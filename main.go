//go:build windows

// Command rig-exporter publishes the FPS reading from RivaTuner Statistics Server,
// the game it belongs to, the display mode and CPU/RAM load to Home Assistant
// over MQTT, and lives in the Windows notification area while it does.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/corgan/rig-exporter/internal/app"
	"github.com/corgan/rig-exporter/internal/applog"
	"github.com/corgan/rig-exporter/internal/collector"
	"github.com/corgan/rig-exporter/internal/config"
	"github.com/corgan/rig-exporter/internal/i18n"
	"github.com/corgan/rig-exporter/internal/metrics"
	"github.com/corgan/rig-exporter/internal/rtss"
	"github.com/corgan/rig-exporter/internal/tray"
	"github.com/corgan/rig-exporter/internal/webui"
	"github.com/corgan/rig-exporter/internal/winapi"
	"golang.org/x/sys/windows/registry"
)

// singleInstanceMutex is per-session, so one instance per logged-in user.
const singleInstanceMutex = `Local\rig-exporter-single-instance`

func main() {
	showVersion := flag.Bool("version", false, "print the version and exit")
	probe := flag.Bool("probe", false, "print one reading in every export format and exit")
	configPath := flag.String("config", "", "path to config.json (default: %APPDATA%\\rig-exporter\\config.json)")
	flag.Parse()

	if *showVersion {
		fmt.Printf("%s %s\n", config.AppName, config.Version)
		return
	}
	if *probe {
		if err := runProbe(*configPath); err != nil {
			fmt.Fprintln(os.Stderr, "probe:", err)
			os.Exit(1)
		}
		return
	}

	// The language of a dialog shown before the configuration is read comes
	// from the configuration anyway: reading it is cheap, and a message box in
	// the wrong language is the first thing a user would notice.
	lang := configuredLanguage(*configPath)

	// Holding the mutex for the process lifetime is what makes the check work,
	// so the handle is intentionally never closed.
	if _, first, err := winapi.AcquireSingleInstance(singleInstanceMutex); err == nil && !first {
		winapi.MessageBox(config.AppName, i18n.T(lang, "dialog.alreadyRunning"),
			winapi.MBOK|winapi.MBIconInformation|winapi.MBSetForeground)
		return
	}

	if err := run(*configPath); err != nil {
		winapi.MessageBox(config.AppName, i18n.T(lang, "dialog.startFailed")+"\n\n"+err.Error(),
			winapi.MBOK|winapi.MBIconWarning|winapi.MBSetForeground)
		os.Exit(1)
	}
}

// configuredLanguage reads just the language out of the configuration, for the
// dialogs that can appear before the full startup has run.
func configuredLanguage(configPath string) i18n.Lang {
	cfg, err := loadConfigFor(configPath)
	if err != nil {
		return i18n.Default
	}
	return cfg.Lang()
}

// runProbe takes one reading and prints it in every export format.
//
// The binary is linked as a GUI application and has no console of its own, so
// this is meant to be redirected: rig-exporter.exe -probe > reading.txt. It is the
// quickest way to tell whether RTSS is readable and what Home Assistant,
// Prometheus or InfluxDB would actually receive.
func runProbe(configPath string) error {
	cfg, err := loadConfigFor(configPath)
	if err != nil {
		return err
	}

	c, start, stop := app.Probe(cfg, applog.Discard())
	start()
	defer stop()

	// Two passes: CPU load, disk throughput and network rates are all
	// differences between samples, so the first one has nothing to report.
	// The wait also gives the latency probe time to finish its first round.
	c.Collect()
	time.Sleep(4 * time.Second)
	snap := c.Collect()

	fmt.Printf("%s %s — node_id %s\n\n", config.AppName, config.Version, cfg.NodeID)

	fmt.Printf("RTSS:       %s", snap.RTSSStatus)
	if snap.RTSSVersion != "" {
		fmt.Printf(" (shared memory v%s)", snap.RTSSVersion)
	}
	if snap.RTSSMessage != "" {
		fmt.Printf(" — %s", snap.RTSSMessage)
	}
	fmt.Printf("\nGame:       %s\n", snap.Game())
	fmt.Printf("FPS:        %.1f (%.2f ms)\n", snap.FPS(), snap.FrametimeMs())
	fmt.Printf("Display:    %s @ %d Hz\n", snap.Resolution(), snap.RefreshHz())
	fmt.Printf("CPU / RAM:  %.1f %% / %.1f %%\n\n", snap.CPUPercent(), snap.RAMPercent())

	lang := cfg.Lang()
	for _, group := range metrics.Groups {
		if group == metrics.GroupCore {
			continue
		}
		if err := snap.SourceErrors[group]; err != "" {
			fmt.Printf("--- %s: unavailable (%s) ---\n\n", group.Label(lang), err)
			continue
		}
		if !snap.HasGroup(group) {
			continue
		}
		fmt.Printf("--- %s ---\n", group.Label(lang))
		for _, reading := range snap.Entities() {
			if reading.Def.PanelGroup() == group {
				fmt.Printf("  %-34s %v %s\n", reading.DisplayName(lang), reading.Value(), reading.Def.Unit)
			}
		}
		fmt.Println()
	}

	state, err := json.MarshalIndent(snap.JSON(), "", "  ")
	if err != nil {
		return err
	}
	fmt.Printf("--- MQTT / JSON (%s) ---\n%s\n\n", cfg.StateTopic(), state)
	fmt.Printf("--- Prometheus ---\n%s\n", snap.Prometheus(cfg.NodeID))
	fmt.Printf("--- InfluxDB line protocol ---\n%s\n", snap.Influx(cfg.InfluxMeasurement, cfg.NodeID, time.Now()))

	if snap.RTSSStatus != collector.RTSSOK {
		fmt.Printf("RTSS is not readable. Download: %s\n", config.RTSSDownloadURL)
	}
	return nil
}

// loadConfigFor resolves the config path and loads it, ignoring a parse error
// in favour of defaults.
func loadConfigFor(configPath string) (config.Config, error) {
	if configPath == "" {
		path, err := config.Path()
		if err != nil {
			return config.Config{}, err
		}
		configPath = path
	}
	cfg, _, err := config.Load(configPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "config could not be read, using defaults:", err)
	}
	return cfg, nil
}

func run(configPath string) error {
	if configPath == "" {
		path, err := config.Path()
		if err != nil {
			return err
		}
		configPath = path
	}

	// A configuration written under the previous application name is adopted
	// before anything reads it, so an upgrade keeps the broker settings.
	migrated, migrateErr := config.MigrateLegacy(configPath)

	cfg, created, cfgErr := config.Load(configPath)

	logPath, err := config.LogPath()
	if err != nil {
		return err
	}
	log, closeLog, err := applog.Setup(logPath, cfg.Debug)
	if err != nil {
		return err
	}
	defer closeLog.Close()

	log.Info("starting", "version", config.Version, "config", configPath, "node_id", cfg.NodeID)
	if cfgErr != nil {
		// A broken config is not fatal: defaults keep the app usable and the
		// settings page lets the user fix it.
		log.Error("config could not be read, using defaults", "error", cfgErr)
		winapi.MessageBox(config.AppName,
			i18n.T(cfg.Lang(), "dialog.configBroken")+"\n\n"+cfgErr.Error(),
			winapi.MBOK|winapi.MBIconWarning)
	}
	if created {
		log.Info("wrote default configuration", "path", configPath)
	}
	if migrateErr != nil {
		log.Warn("could not migrate the previous configuration", "error", migrateErr)
	}
	if migrated {
		log.Info("migrated the configuration from the previous application name",
			"from", config.LegacyAppName, "path", configPath)
	}

	application := app.New(cfg, configPath, log)

	settings, err := webui.New(application, log)
	if err != nil {
		return err
	}
	if err := settings.Start(); err != nil {
		// The settings page is a convenience; losing it must not stop the
		// actual job of publishing to Home Assistant.
		log.Error("settings ui unavailable", "error", err)
	}

	application.Start()

	// Only now, with the exporters running and the settings page reachable: a
	// machine without RTSS is a perfectly good machine for everything else this
	// tool does, and nothing it does should wait behind a dialog.
	warnIfRTSSMissing(application, log, cfg.Lang(), created)

	trayUI := tray.New(application, log, tray.Options{
		SettingsURL: settings.URL,
		LogPath:     logPath,
		OnQuit: func() {
			log.Info("shutting down")
			application.Stop()
			settings.Stop()
		},
	})
	trayUI.Run() // blocks until the user picks Beenden

	return nil
}

// warnIfRTSSMissing tells the user where to get RTSS when the shared memory
// cannot be read. A permission problem gets its own wording, because
// downloading RTSS again would not help.
//
// The prompt appears on the first run only. RTSS is missing on every machine
// that is not a gaming PC — a server, a virtual machine, a laptop — and this
// used to mean a modal dialog at every single logon, before the exporters or
// the settings page had started. Once the user has been told, the tray icon and
// the status page carry the state; both render collector.RTSSStatus already,
// which is where someone wondering about missing FPS will look.
func warnIfRTSSMissing(application *app.App, log *slog.Logger, lang i18n.Lang, firstRun bool) {
	err := application.CheckRTSS()
	if err == nil {
		log.Info("rtss shared memory available")
		return
	}
	log.Warn("rtss unavailable at startup", "error", err)
	if !firstRun {
		return
	}

	if errors.Is(err, rtss.ErrAccessDenied) {
		winapi.MessageBox(config.AppName, i18n.T(lang, "dialog.rtssDenied"),
			winapi.MBOK|winapi.MBIconWarning|winapi.MBSetForeground)
		return
	}

	// Telling someone to download software they already have is worse than
	// saying nothing, so the download prompt is only for machines that really
	// do not have it.
	if rtssInstalled() {
		winapi.MessageBox(config.AppName, i18n.T(lang, "dialog.rtssNotRunning"),
			winapi.MBOK|winapi.MBIconInformation|winapi.MBSetForeground)
		return
	}

	answer := winapi.MessageBox(config.AppName, i18n.T(lang, "dialog.rtssMissing"),
		winapi.MBYesNo|winapi.MBIconWarning|winapi.MBSetForeground)

	if answer == winapi.IDYes {
		if err := winapi.OpenURL(config.RTSSDownloadURL); err != nil {
			log.Error("could not open the RTSS download page", "error", err)
		}
	}
}

// rtssInstalled reports whether RTSS is on this machine, however it got here —
// on its own or carried in by MSI Afterburner.
//
// RTSS is a 32-bit program, so on 64-bit Windows its key sits under
// WOW6432Node and the plain path finds nothing. Both are tried, since the plain
// one is right on a 32-bit Windows.
//
// The key existing is the whole answer. Its values are read from one RTSS
// version here and a future one may rename them, so a missing value must never
// be mistaken for a missing installation.
func rtssInstalled() bool {
	for _, path := range []string{
		`SOFTWARE\WOW6432Node\Unwinder\RTSS`,
		`SOFTWARE\Unwinder\RTSS`,
	} {
		key, err := registry.OpenKey(registry.LOCAL_MACHINE, path, registry.QUERY_VALUE)
		if err != nil {
			continue
		}
		key.Close()
		return true
	}
	return false
}
