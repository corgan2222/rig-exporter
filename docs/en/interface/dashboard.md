# Dashboard

![The dashboard page with tiles, state switches and one panel per piece of hardware](../../images/screenshots/en/dashboard.png)

Live readings, the state of the export targets, one panel per sensor group and
the addresses of the active endpoints. Refreshes at the read interval. The only
page without settings — apart from two switches that sit here because this is
where you notice you need them.

Under the tiles, four chips say what is set right now: which scope, whether
decimals are on, how many entities are created, and how often it publishes —
and that is the rate in force **right now**, with "in game" or "idle" added. A
number without that addition would be worthless, because there are
two of them. Every chip leads to the setting that determines it; reading a value
and changing it should not be two searches. Tiles for values the chosen scope
does not measure at all are hidden rather than shown empty.

The hardware panels can be switched between two views. The default is **By
device**: everything about GPU 0 together, then everything about GPU 1, each
disk on its own, each adapter on its own — every group under a heading carrying
the device name, the values below it named only briefly. **By measurement**
leaves those headings out and writes the device into every single row instead.
The order is the same either way, device by device; only where the device name
sits changes. The choice is kept in the browser.

Where a machine has no GPU, or is deliberately not used for game data, the RTSS
notice can be cleared away for good. The button reads **"No GPU present — hide
game status"** when Windows reports no graphics card at all, and **"Not used for
gaming — hide game status"** when there is one: the same switch, but telling a
Radeon that it is not present would simply be wrong. The setting is stored as
`no_gpu` in `config.json` and additionally hides the FPS, Frame time and Game
tiles along with the RTSS status chip. It can be switched off again under
[*Export & display → Application*](export-and-display.md#application). Collection
and exports do not change because of it.
