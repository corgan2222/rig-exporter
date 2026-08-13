# What applies to every page

On the two pages with forms — *Data capture* and *Export & display* — every box
of settings has its own save button, which only turns green once something has
been changed in exactly that box. And only that one box is saved: a form
carries no record of the checkboxes it does not contain, and a partial apply
would otherwise switch off everything on the other page. *Measurements* has no
save button at all: there every change takes effect immediately at the place
where it is made, and the header confirms it briefly with "applied".

Every box can be **collapsed** by its heading, and the page remembers what was
closed — across a restart as well. That deliberately sits in the browser and
not in the configuration: if the configured port is lost, the interface falls
back to a random one, and a different port is a different origin, which is what
the storage hangs on. A view preference may be lost that way; the answer to a
question may not, and that one is in the configuration. A link to a collapsed
box opens it again.

The language switch is on the right in the header. It affects the interface,
the tray menu, dialogs and the entity names shown in Home Assistant. What it
explicitly does **not** touch are the identifiers: `default_entity_id`,
`object_id`, `unique_id` and the value template stay the same, because
dashboards and automations hang on them. An entity id like
`sensor.re_corganpc2_fps` is the same in both languages, only the displayed
name changes. Dashboards and automations therefore survive a language change
unharmed.

What machines read is not translated: Prometheus help texts, log lines and
error messages stay English. The same goes for reported *values* — `Ethernet`,
`Wi-Fi`, `Other`, `DDR4`, `Type 126` stay English, so that an automation does
not depend on which language happens to be set.

At the bottom of every page, three buttons open the configuration, the log and
the folder around them. The detour via the server is necessary because a
browser will not follow a `file://` link from an `http` page.

Next to them, **Help** opens this handbook — in whichever language the interface
is set to, so a German interface lands on the German pages. It is a link rather
than a button because it leaves the machine, and that difference should be
visible in the browser's status bar before the click rather than after it.
