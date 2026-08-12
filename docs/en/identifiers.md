# Identifiers and filing

What a measurement is called, and where Home Assistant puts it.

## How the identifiers are built

An identifier names the **device** first, then the **quantity**:

```
diskc_used_percent      gpu0_temperature      net_ethernet_2_rx
diskd_free              gpu1_vram_used        net_ethernet_2_link
```

That way all the values of one drive stand together. Device and number grow
into a single word — `gpu0`, `diskc` read as one unit. Only where the instance
is itself multi-part does the separator stay: `netethernet_2` would be
unreadable.

In Home Assistant, who supplies the value and from which machine comes in front
of that:

```
sensor.re_corganpc2_gpu0_vendor
sensor.re_corganpc2_diskc_free
```

`re` stands for rig-exporter. From left to right the id thus answers exactly
the questions in the order you ask them when skimming a list of a hundred
entities: which program, which machine, which hardware, which measurement.

It is not right everywhere. A processor core is counted by the word `cpu_core`,
so `cpu_core_5` already reads correctly — `cpu_5_core` would be nonsense. The
same holds for memory modules. What was turned around are the four dimensions
where the device belongs in front of the quantity: graphics cards, drives,
network adapters and cooling controllers.

**If a spelling does change after all**, the program clears the earlier one off
the broker itself at the next connection — so the entity disappears from Home
Assistant instead of standing there as "unavailable". Dashboards and
automations pointing at the old name then have to be moved over.

That is necessary because a discovery message is **retained**: it lies on the
broker and outlives both this program and the deletion of the entity by hand —
which simply comes back at the next restart of Home Assistant. So every old
name is explicitly withdrawn with an empty message. Writing into a topic that
never existed does nothing, so this needs no migration flag and no memory.

## Where Home Assistant files the values

67 measurements stand in the main area, 48 under **Diagnostic**, 7 are not
published as an entity at all. Counted across the full catalogue — what arrives
on a particular machine depends on its hardware and on the selection. The rule
behind it:

* **Diagnostic** — facts *about* the machine instead of measurements *on* it:
  model, vendor, file system, capacity, slots, nominal and limit values,
  Windows version. Everything you look at when hunting a fault and that does
  not move by itself. Home Assistant keeps that out of the main list and out of
  automatically generated dashboards.
* **Main area** — what the machine is doing right now: frames per second,
  temperatures, load, free space, throughput, power.

The borderline cases are decided by what they are for, not by their form. The
display mode is configuration in principle, but a refresh rate that has quietly
dropped to 60 Hz is exactly what belongs on a dashboard — so, main area. Idle
time drives presence automations and likewise stays at the top, while uptime
answers the question "how long since the last restart" and is diagnostic. Same
form, different job.

The filing is pinned down in `testdata/catalogue.txt`: refiling a value moves
it out of the main list in Home Assistant, and that should stand out in review
rather than at the user.
