# Interface

The interface lives at `http://127.0.0.1:8787` and has four pages, reachable
from the header. Roughly divided: **Dashboard** shows, **Data capture** decides
*which hardware* is read, **Measurements** decides *which of those values* are
sent, and **Export & display** decides *where to*.

That order is also the sensible reading order: hardware that is switched off on
the second page never appears on the third in the first place.

| Page | What is decided there |
|---|---|
| [Dashboard](dashboard.md) | Nothing — this is where things are shown. Except for two switches that you need exactly here |
| [Data capture](data-capture.md) | Which hardware is read at all |
| [Measurements](measurements.md) | Which of the values read are sent, and how often |
| [Export & display](export-and-display.md) | The fields for MQTT, data server and InfluxDB — and how the program itself behaves |

Plus two pages that apply to all four:
[What applies to every page](common.md) and
[The interface on the network](on-the-network.md). Two more hang off them and sit
one level up because they are extensive enough:
[Reading and sending](../polling-and-publishing.md) and the
[tray menu](../tray-menu.md).

What arrives on the other side — topics, endpoints, the configuration of the
receiver — is under [Export targets](../export-targets.md).

![The Dashboard page, the one the interface starts on](../../images/screenshots/en/dashboard.png)
