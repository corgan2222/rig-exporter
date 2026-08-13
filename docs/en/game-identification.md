# Game identification

RTSS reports the executable. `Cyberpunk2077.exe` is what the **Game**
measurement has always published, and a file name is all it is — the spelling
somebody's build system happened to pick. The game is called *Cyberpunk 2077*,
and its artwork is addressed by neither of those two names but by a number: the
Steam app id `1091500`.

Nothing turns one into the other. `SpaceMarine2.exe` is *Warhammer 40,000:
Space Marine 2*, `bl3.exe` is *Borderlands 3*, `DOOM64.exe` is *DOOM 64* with a
space in it. Split the file name, drop the digits, insert capitals — however
clever the rule, the result is a guess, and a guess is exactly what must not be
published here. The answer has to be looked up, and the only parties holding it
are the launcher that installed the game and the store that sells it.

**Try to work out the game's name and Steam app id** on the
[Data capture](interface/data-capture.md#working-out-the-game) page is that
lookup. It is off by default, it is marked `ALPHA · internet`, and it is the
only setting in this program that contacts somebody else's server.

## Three sources, cheapest first

1. **Steam, through the registry.** Steam writes the app it launched into
   `HKCU\Software\Valve\Steam\RunningAppID` and the title it keeps for that app
   into `…\Steam\Apps\<id>\Name`. Two reads: no elevation, no access to the
   game's process, nothing that leaves the machine. For a Steam game that is
   the whole answer — the title and the id arrive together.
2. **The GOG and Epic catalogues, on disk.** Neither launcher says what is
   *running*; both say what is *installed*, and where. So the question is
   turned around: the executable path RTSS reported is matched against the
   install folders GOG keeps in the registry and Epic keeps as manifest files.
   The longest matching folder wins, so a game installed inside another game's
   directory is reported as itself rather than as its host.
3. **Steam's public store search.** Asked in two situations: for the app id of a
   title step 2 named, and — when neither step above named anything at all — for
   a term worked out from the executable itself. `Cyberpunk2077.exe` becomes the
   question "Cyberpunk 2077"; `MyGame-Win64-Shipping.exe` becomes "My Game".
   That last case is the only guess in the whole mechanism, and it is a guess
   about what to **ask**, never about what to report: what gets published is the
   title the store answers with and the id it gave, or nothing at all. A game
   bought outside these three shops otherwise has no chance of an app id, and the
   app id is what fetches the artwork.

   Programs that are never games — browsers, chat windows, capture tools, the
   launchers themselves — are not asked about at all. RTSS hooks whatever
   presents frames, so they turn up here as readily as a game does, and
   "Origin" would come back as a game called Origin.

The order is a cost ladder that happens to be a certainty ladder as well.
Steam's answer is the launcher's own record of what it started — exact, local,
free. GOG and Epic cost a registry enumeration and a directory of small files,
and their answer is a path match: right unless a game sits somewhere very
strange. The store's answer is a *search*, the best match for a term, and it is
both the only one of the three that can be wrong and the only one that leaves
the machine. Cheapest first therefore also means: ask the fallible one last, and
only when the others have nothing to say.

A path no catalogue claims is worth one reread, at most every five minutes —
that is what a game installed while the program runs looks like, and "I
installed it, why is it not there" is a report nobody can act on. An executable
already known either way never reaches that point.

Two further ways of asking Steam were measured and dropped; the reasons are
under [How the values come about](how-values-are-obtained.md).

## Add-ons live in the base game's folder

This is the trap that produces the wrong picture, and it is worth knowing that
it was handled rather than missed.

Both GOG and Epic list an add-on in the same catalogue as the game it extends,
pointing at the same folder. On the machine this was built on, three GOG entries
name the *Cyberpunk 2077* directory: the game, *Phantom Liberty* and *REDmod*.
Match a path against that list naively and any of the three may win — and the
consequence is not a missing icon. Send "Cyberpunk 2077: Phantom Liberty" to the
store search and it answers with the expansion's own app id, which Home
Assistant then shows with complete confidence.

Each launcher marks the difference in its own way. Both were read off real
installations rather than assumed:

| Launcher | How an add-on gives itself away |
|---|---|
| **GOG** | it carries `dependsOn`, naming the game it extends, and has no `exe` of its own |
| **Epic** | it has no `LaunchExecutable`. `MainGameAppName` looks like the obvious test and is not one — *DOOM 64* leaves it empty and is very much a game |

Only entries that pass those tests reach the path match at all.

## What leaves the machine, and what does not

Steps 1 and 2 are a handful of registry reads and a few small files. Step 3 is
one HTTPS request to Steam's public store search — the same endpoint the search
box on the store page uses, with no key, no account and no sign-in. What goes
out is the game's title, or — where no launcher named one — a term worked out
from the executable, which is a name you chose to install. Nothing else: no
machine name, no hardware, no configuration, no identifier of any kind. What
comes back is an app id and the store's spelling of the title.

It is asked **once per term**, and the answer is kept — **including the
misses**. A game the store has never heard of is precisely the case that would
otherwise ask again on every poll, twice a second, for as long as the game is
open.

It is never waited for. The lookup runs beside the measurement loop, so a title
whose id has not arrived yet is published without one and gains it on a later
reading. A slow store must not become a slow exporter.

What is remembered lives in memory only. Nothing is written to disk; restarting
the program — or changing any setting, which rebuilds the collector — forgets
everything learned, and that is also the only way to clear it.

**One switch, not three.** With the option off nothing is identified at all:
not the store, and not the two local sources either. The **Game** measurement is
then the executable and nothing else, exactly as it was before this feature
existed.

## Missing rather than guessed

An executable the store does not recognise either produces no details at all. A
game the store does not have keeps its platform and its title and simply has no
app id. And a game found by name alone has a title and an id but **no
platform** — no launcher claimed it, so there is nothing to report there. There
are no empty strings, no zeroes and no "unknown" anywhere in this: the same rule
as everywhere else in the program, because a value that is there claims
something and an absent one does not.

For an app id the rule is stricter than a preference. A wrong id is not a
missing picture — it is the wrong game's picture, and no picture beats that.

## On a Home Assistant dashboard

The **Game** entity is untouched: its state is still the executable, its entity
id is unchanged, and every automation built on it keeps working. Platform, title
and app id arrive as **attributes** of that same entity. What the message looks
like is under [Export targets](export-targets.md#game-attributes).

| Attribute | Example | What it is |
|---|---|---|
| `platform` | `steam`, `gog`, `epic` | an identifier, lower case, never translated |
| `title` | `Cyberpunk 2077` | as the store spells it, punctuation and all |
| `app_id` | `1091500` | Steam's id for the title — what addresses the artwork |

Steam serves the artwork for an app id straight from its CDN:

```
https://cdn.cloudflare.steamstatic.com/steam/apps/<appid>/header.jpg
```

Five files are useful, and which one to reach for depends on the shape of the
slot on the dashboard:

| File | What it is |
|---|---|
| `header.jpg` | the wide banner, the usual choice for a card |
| `capsule_231x87.jpg` | the small wide capsule, for a tile-sized slot |
| `library_600x900.jpg` | the upright cover, the shape of a library grid |
| `library_hero.jpg` | the wide backdrop, for a page or card background |
| `logo.png` | the title lettering alone, transparent, to lay over the hero |

Measured against `1091500`: all five answer over plain HTTPS with no key, no
account and no cookie.

A Markdown card is enough to show one, and needs nothing from HACS:

```yaml
type: markdown
entity_id:
  - sensor.re_corganpc2_game
content: |
  {% set id = state_attr('sensor.re_corganpc2_game','app_id') %}
  {% if id %}![](https://cdn.cloudflare.steamstatic.com/steam/apps/{{ id }}/header.jpg)

  **{{ state_attr('sensor.re_corganpc2_game','title') }}**{% else %}_nothing recognised_{% endif %}
```

The `{% if id %}` is not politeness. The attributes are cleared as soon as
nothing is recognised any more, which is what stops a closed game's cover from
staying on the dashboard — and a card that assumes the attribute is there would
render a broken image instead. More cards are under
[Home Assistant](home-assistant.md#card-configuration).

## What alpha means here

Three launchers are read: Steam, GOG Galaxy and Epic. Everyone else's — Ubisoft
Connect, EA app, Battle.net, itch.io, a game installed by hand — is not, and a
game from any of them stays an executable.

**The launcher catalogues were read on one machine.** One Steam installation,
one GOG Galaxy, one Epic launcher, with the games that happened to be on it.
The tests that separate a game from an add-on, and the fields the folders are
read out of, are what those installations actually contain; another machine may
spell something differently or carry a field this does not expect. That is the
part that needs other people's PCs before it stops being alpha.

Two known limitations on top of that:

* **The store's answer is its best match**, not a certainty. A title with an
  edition, a bundle or a re-release in the way can land on a neighbouring app
  id.
* **Steam reports the app it launched, not the window in front.** A Steam game
  left running in the background therefore outranks whatever RTSS is currently
  drawing. It is a rare state, and the alternative — guessing from the path
  which of two running games is meant — would be worse.

Anything that turns out wrong is a wrong *attribute*. The state of the **Game**
entity, its identity and its history are not affected by any of this.
