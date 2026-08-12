# Development

For everybody who wants to touch the source — or merely wants to know what
actually happens when it is built.

<div class="grid cards" markdown>

-   :material-hammer-wrench: **[Building it yourself](../building.md)**

    Go, one script, no C compiler. What `-Check`, `-Race` and `-Icon` do and why
    those are three separate flags.

-   :material-plus-box-outline: **[Adding your own measurements](../custom-measurements.md)**

    Report a value the program does not know yet. Four steps, and not one line
    of export code.

</div>

The repository lives on [GitHub](https://github.com/corgan2222/rig-exporter).
Every change goes through a pull request, and the CI runs the same
`build.ps1 -Check` that you can run locally — so there is no surprise that only
shows up on the server.
