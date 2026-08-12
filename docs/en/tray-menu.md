# Tray menu

Shows FPS, Game, Display and Load including the GPU live, plus one status line
per active export target and the RTSS status. Further actions: Pause exporting,
Settings…, Open log, Start with Windows, Quit. If RTSS is missing, an entry for
downloading it is added.

Right at the top are the name, the version and the address at which the
interface can be reached —
`rig-exporter 1.10.1+<commits>.<hash> — 127.0.0.1:8787`. A click on it opens the
interface. The address is there because it is not always the configured one: if
the port is taken, the server falls back to a random one, and then this is the
only place where the correct number stands.
