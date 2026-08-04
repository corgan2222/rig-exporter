<!--
Branch prefixes decide the label and the release notes: feature/ fix/ docs/
test/ chore/. See CONTRIBUTING.md.
-->

## What this changes, and why

<!--
The why matters more than the what — the diff already says what. What was
wrong, or what was missing?
-->

## How it was verified

<!--
Measured beats assumed. Against real hardware or a real broker where possible,
with the numbers. Keep numbers from your own machine out of the shipped
documentation and the interface, though: there they read as a claim about
somebody else's PC.
-->

- [ ] `.\build.ps1 -Check` is green (gofmt, vet, staticcheck, tests, build)

## The measurement contract

<!--
internal/metrics/testdata/catalogue.txt pins every identifier, unit, kind,
precision, group, panel, category and Prometheus name.
-->

- [ ] Untouched
- [ ] Changed deliberately, recorded with `go test ./internal/metrics/ -update-catalogue`

If it changed, which consumers have to be repointed — Home Assistant entities,
Prometheus rules, InfluxDB dashboards?

## Invariants

- [ ] Identifiers translate nowhere; only displayed names follow the language
- [ ] The output is identical whatever data source produced a value
- [ ] Values that could not be read are omitted, not reported as zero
