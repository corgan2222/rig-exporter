# The interface on the network

By default the interface listens on `127.0.0.1` only — reachable from this
machine and from nobody else. **Make this page reachable on the network** under
*Application* binds it to `0.0.0.0` instead, which leaves it open on this PC's
LAN address. Takes effect after a restart, like the port change above it.

![The Application box with the port and the network switch](../../images/screenshots/en/export-app.png)

> **What that means:** the interface has **no login**. Whoever reaches it can
> change every setting. The stored credentials are not visible to them — the
> page never shows them — but they could redirect the target those credentials
> are sent to. That is exactly what this rule is for: **a secret does not
> travel.** If the broker address or the InfluxDB URL changes without the
> password, or the token, coming along in the same submission, they are dropped
> instead of being sent to the new address. That is the reason they have to be
> entered again after an address change.
>
> Even so: switch it on only on a network you trust, otherwise put it behind a
> reverse proxy with a login — and never forward it to the internet. When in
> doubt leave it off; the default is loopback and that one is right.

A second bolt sits in front of this, and it holds on plain loopback too:
submitting a form is a CORS simple request and needs no permission, so a web
page you are visiting could simply send something here. The interface therefore
accepts a changing request only when the browser marks it as coming from this
page; everything else gets a 403. Reading is not affected by that, so a bookmark
keeps working.

Two things follow along automatically: the topmost tray entry and the “Visit”
link on the Home Assistant device page then point at the LAN address instead of
at `127.0.0.1`. That makes the link work from a phone as well. The address is
determined from the default route rather than built from the machine name,
because an address also works where name resolution on the local network does
not.

The data server further up the same page is independent of this: it has always
listened on `0.0.0.0`, but it knows a token.
