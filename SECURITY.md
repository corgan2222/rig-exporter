# Security policy

rig-exporter is a desktop program. It runs as the logged-in user on one Windows
PC, reads sensors, and hands the readings to Home Assistant, Prometheus or
InfluxDB. It is not a service, it has no accounts, and it serves nobody but the
person sitting in front of the machine. That decides what counts as a
vulnerability here, so the scope is written out below instead of left to
guessing.

## Supported versions

Only the latest release.

| Version | Fixes |
|---|---|
| Latest release | Yes |
| Anything older | No — update first |

One person maintains this project. There is no branch where an older version
keeps receiving patches, and a support matrix claiming otherwise would be a
promise nobody can keep. The program asks GitHub for a newer release every six
hours and offers it in the interface, so being current costs a click. A report
against an older build will be met with the question whether it still happens on
the current one.

## Reporting a vulnerability

**Please do not open a public issue first.** Issues here are public the moment
they are filed, and every reader of one is somebody who can act on it before a
fix exists.

Two private ways, either is fine:

- **GitHub private vulnerability reporting** — the *Security* tab of this
  repository, *Report a vulnerability*. Preferred: the report, the discussion
  and the fix stay in one place, and you can see the patch before it is public.
- **Email `stefan@knaak.org`** with `rig-exporter security` in the subject line.
  Nothing is encrypted at rest on the receiving end; if the detail is too
  sensitive for plain mail, send a short note asking for another channel.

What makes a report fast to act on: the version shown in the interface, the
Windows build, which listeners you had switched on (settings interface, data
server, MQTT, InfluxDB push), and the shortest way to trigger it. `-probe` output
is welcome, but **read it before sending** — it names your machine, its drives
and its network addresses.

## What to expect

One maintainer, in a European time zone, working on this outside a job. The
numbers below are what one person can actually hold to:

- **Acknowledgement within 7 days.** If nothing arrives, send it again. The mail
  was lost or filed as spam; a second one is not a nuisance.
- **An assessment within 30 days** — whether it is in scope, how bad it looks,
  and whether it will be fixed.
- **A fix in the next release** for anything that reaches the machine or the
  stored credentials from off it. Everything else queues with the rest of the
  work.
- **Credit in the release notes** under whatever name you give, unless you would
  rather not be named.

There is no bounty and no payment. If a report sits unanswered for 90 days,
publish it — a policy that asks for silence without holding up its own end has
not earned it.

## What is worth looking at

The interesting parts, and what each already does. A way around any of these is
a report worth sending.

**The settings interface** (`internal/webui`) shows every setting, including the
fields that decide where the broker password and the InfluxDB token are sent. It
binds `127.0.0.1` by default; `web_bind_all` moves it to `0.0.0.0` and is off
until somebody switches it on. Every request that changes something passes a
same-origin check (`samesite.go`), because a form post is a CORS simple request:
a web page you happen to be visiting can post to a loopback port with no
preflight and no permission asked, and not being able to read the answer does not
stop the request from arriving. Ways past that check are in scope on loopback
just as much as on a LAN bind.

**The data server** (`internal/export/dataserver`) is off by default and binds
`0.0.0.0:9838` when switched on, because Prometheus and Home Assistant scrape it
from another host. It serves readings only, never configuration. `data_token`,
when set, guards every endpoint except `/health`, accepted either as a bearer
header or as `?token=`, and compared in constant time.

**Stored credentials** — the MQTT password, the InfluxDB token and the
data-server token — live in `%APPDATA%\rig-exporter\config.json` in plain text,
written atomically with owner-only permissions. Three things already hold, and
breaking any of them is a finding: the interface never sends a stored secret back
to the browser (an empty field means keep what is stored); a secret is dropped
rather than carried when its target changes, so moving the broker host or port,
or the InfluxDB URL, requires entering it again; and crash reports have both a
`key=value` filter and a URL-userinfo filter run over them before anything is
offered for publication.

**The self-updater** (`internal/updater`) downloads a size-bounded release
archive from this repository and verifies an ECDSA signature over `checksums.txt`
against a certificate compiled into the binary, then the archive against that
checksum file. The matching private key exists only as a repository secret and
ships with nothing. If either check fails, the download is discarded and no file
on disk is touched. Past that point the staged executable is hashed again
immediately before the swap, the running EXE is copied aside first, and the new
build has to report its own version back through a marker file — if it does not
start, or starts and stays silent, the backup is put back and restarted. The
check for a new version runs on its own; the install never does, it needs the
button.

**PawnIO** (`internal/hardware/pawnio`) is the one optional kernel driver and the
only reason to run this program elevated. Off by default. It is neither shipped
nor installed here; the modules are fetched from the upstream release pinned in
`modules.go`, and the driver verifies each module's signature itself before
loading it, which is why a tampered download does not run. Everything else works
without administrator rights.

**Crash reports** (`internal/crashlog`) are written to `%APPDATA%\rig-exporter`
and stay there. The interface can prepare a filled-in GitHub issue, but that is a
page in your own browser that you read word for word before you submit it.
Nothing is transmitted otherwise.

## Out of scope

Not because they do not matter, but because they are documented decisions.
Reporting one tells nobody anything that is not already written down.

- **The settings interface has no login.** On loopback, anyone who can reach the
  port can already read `config.json` and the process's memory, so a password
  stored on the same disk buys nothing. The same-origin check is the actual
  defence, and that one *is* in scope.
- **`web_bind_all` puts the settings on the network.** It is off, it is its own
  switch, and turning it on is a statement that you trust that network.
- **The data server without a token.** Leaving `data_token` empty is a choice,
  and the endpoints behind it serve readings, not settings.
- **Credentials in `config.json` are not encrypted.** There is no key to encrypt
  them with that would not sit on the same disk under the same account. The file
  permissions are the protection. Reading them out of the process, or out of a
  listener, is a different matter and is in scope.
- **`mqtt_tls_insecure` skips certificate verification.** Off by default,
  labelled as what it is, and there for self-signed brokers on a home LAN.
- **Running elevated because PawnIO was switched on.** The elevation is the point
  of that switch. A way to make the program do something *else* with those rights
  is in scope.
- **Bugs in what the machine already runs** — RTSS, MSI Afterburner, NVML, ADLX,
  the PawnIO modules, the Windows performance counters. Those belong to their
  projects. This program mishandling what they return belongs here.
- **Defender or SmartScreen warning about an unsigned build.** A false positive,
  not a vulnerability. The release binary and its checksums are on the releases
  page.
- **Scanner output with no path to an effect on this program**, including
  dependency advisories for code this binary does not compile in. Name the call
  that reaches it and it becomes a report.
- **Denial of service by somebody already logged in at the machine.** They can
  close the program from the tray.
