# First start

1. Start `rig-exporter.exe` — a speedometer icon appears in the notification
   area, and the interface opens in the default browser.
2. Choose an export target and the sensor groups, then **Save and apply**.

Configuration and log live in `%APPDATA%\rig-exporter`.

The browser opens **only on a start by hand**. The autostart entry carries
`-background`, so it stays at the tray icon: an unasked-for browser window at
every logon is the fastest way to get autostart switched off again. Anyone who
wants to reproduce that behaviour starts with `-background` themselves.

There are four flags on the command line, everything else is in the interface:

| Flag | Effect |
|---|---|
| `-version` | prints name and version and exits |
| `-probe` | takes one reading, prints it in every format, exits |
| `-background` | starts without opening the browser |
| `-config <path>` | uses this file instead of `%APPDATA%\rig-exporter\config.json` |

`-version` and `-probe` run before the check for an already running instance —
so they also work alongside a running exporter. A normal start does not: a
second instance reports itself with a notice and exits.
