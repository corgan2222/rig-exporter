# First start

1. Start `rig-exporter.exe` — a speedometer icon appears in the notification
   area, and the interface opens in the default browser.
2. Choose an export target and the sensor groups, then **Save and apply**.

Configuration and log live in `%APPDATA%\rig-exporter`.

The browser opens **only on a start by hand**. The autostart entry carries
`-background`, so it stays at the tray icon: an unasked-for browser window at
every logon is the fastest way to get autostart switched off again. Anyone who
wants to reproduce that behaviour starts with `-background` themselves.

There are five flags on the command line, everything else is in the interface:

| Flag | Effect |
|---|---|
| `-version` | prints name and version and exits |
| `-probe` | takes one reading, prints it in every format, exits |
| `-background` | starts without opening the browser |
| `-config <path>` | uses this file instead of `%APPDATA%\rig-exporter\config.json` |
| `-demomode` | adds a simulated game to the tray menu — see below |

`-version` and `-probe` run before the check for an already running instance —
so they also work alongside a running exporter. A normal start does not: a
second instance reports itself with a notice and exits.

### Demo mode

`-demomode` is for building and showing a dashboard without a game running. It
adds three entries to the tray menu, and nothing else changes until one of them
is used:

| Entry | Effect |
|---|---|
| **Demo: simulate a game** | switches the simulation on and off |
| **Demo: game** | Red Dead Redemption 2, Grand Theft Auto V or Cyberpunk 2077 |
| **Demo: store** | which launcher the game claims to come from: `steam`, `gog` or `epic` |

Switched on, the simulated game takes the place of whatever RTSS reports: a
frame rate that wanders around a value typical for the chosen title, the
executable as the running game, and — also with
[game identification](game-identification.md) switched off — platform, title
and the real Steam app id, so the artwork resolves.

!!! warning "These values are not measured"
    They travel the ordinary way into every export that is switched on, MQTT
    and InfluxDB included, and end up in the history there like real ones. Use
    a test broker, or expect a game in your statistics that never ran.

Without the flag the menu entries do not exist and the simulation is not part
of the running program at all.
