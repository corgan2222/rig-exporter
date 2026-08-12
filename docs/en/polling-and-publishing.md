# Reading and publishing

![Scope and rates on the Measurements page](images/screenshots/en/measurements-rung.png)

One rate for reading, two for publishing:

| Setting | Meaning | Default |
|---|---|---|
| **Read interval** | how often the hardware is asked | 500 ms |
| **Publish interval in game** | how often a reading leaves the machine while a game is delivering frames | 2000 ms |
| **Publish interval when idle** | the same when nothing is being rendered | 10000 ms |
| **Idle timeout** | how long a game may deliver no frame before it counts as finished | 3000 ms |
| **Calculate decimal places** | whether numbers are sent with decimal places | on |

The three intervals lie between 250 and 300000 ms, the idle timeout between 500
and 60000 ms; anything outside that is caught when saving.

The reading decides how smoothly the tray and the Dashboard run; the publishing
decides how much arrives at the broker and the time-series database. Anyone who
wants a lively FPS number in the tray without flooding Home Assistant sets the
reading to 250 ms and leaves the publishing where it is.

Which of the two publish rates applies is decided anew at every reading and not
once at startup: a game starting up switches to the fast rate at the next read,
a closed one switches back at the next. Only what actually delivers frames
counts as "a game is running" — RTSS keeps an entry open for a moment after the
last frame, and a game standing still has nothing to say that would be worth a
fast rate.

Both publish intervals are rounded up to a whole multiple of the read interval
and are never shorter than it. Counting happens in readings, not on a second
clock — so the rates cannot drift apart. Among themselves they are independent;
anyone who wants to publish more often when idle than in game may do so.

On the page **Export & display**, between Home Assistant and Data server, sits
the box
[**Long-term storage**](interface/export-and-display.md#long-term-storage-in-home-assistant),
which explains the third adjustment and prints the matching `recorder:` block
for this PC along with it. The block is built from the entities that really
exist right now: two graphics cards give two temperature lines, a switched-off
sensor group none. It belongs in Home Assistant's `configuration.yaml` and needs
a restart there.

Important with it: an exclusion takes an entity's long-term statistics **as
well**, not only its history. It is both or neither.

**Calculate decimal places** is the second lever against a database filling up.
Switched off, the numbers are calculated and sent whole — in every format, not
only in MQTT, so that Prometheus and Home Assistant do not disagree about the
same reading. A value then has to move by a full unit before it counts as
changed at all, and Home Assistant writes only changes into its database.
Discovery reports the matching precision with it, otherwise the interface would
show `x.0` everywhere.

The two rankings of top processes are exempt: they keep their decimal places no
matter how the switch stands. Why, is under
[Decimal places](diagnostics.md#decimal-places-always-here-and-two-for-the-cpu).

---
