# Testing visual-telemetry

The suite is what CI runs and what a change is judged by. A lap posted to a running server is what
a driver would see, and the only way to judge a chart by eye.

## The suite

Everything is a Makefile target, and `make` on its own is `make check`.

| | What | Needs |
|---|---|---|
| `make check` | format, build, vet in both build modes, lint, the tests without a database, every benchmark once, `go mod tidy` is a no-op | the tools below |
| `make test-postgres PACENOTE_TEST_DATABASE_URL=postgres://localhost:5432/postgres` | the whole suite, the store included, with the race detector | a PostgreSQL you may create schemas on — the tests make one each and drop it |
| `make cover PACENOTE_TEST_DATABASE_URL=…` | coverage with the store: every package over 90 % (`cmd/visual-telemetry` excluded — `main()` is the go-plugin handshake, which only a host can run) | the same |
| `make bench` | what a lap costs: reading an upload, drawing the document, drawing the picture | — |
| `make vuln` | govulncheck | — |
| `make dist` | the folder to drop into the server's plugin directory, as `dist/visual-telemetry/` | — |

Tools: `gofumpt`, `golangci-lint` v2, `govulncheck`, each under `$(go env GOPATH)/bin`. CI runs the
same targets with a PostgreSQL 17 service, on Go 1.26 and stable, and govulncheck in a job of its own.

Without a database the suite passes at about 77 %; the store is the rest. With one it is over 90 %,
and that is the number the gate holds.

## What the suite asserts

- **The contract.** An upload is read as the README says: fields present and absent, the trace
  sorted by distance, two to 4 096 points, unknown fields ignored, and every refusal names its
  reason. A chart address is `<stint>/<lap>` and nothing else.
- **The series.** Distance, time at a distance, sector times, top speed, the time lost to another
  lap at every metre, the position on the map: each against a lap built by hand where the answer is
  known.
- **The chart, twice.** The same drawing code draws the document and the picture. The document is
  checked as text — the panels, the sector marks, the turn numbers, the legend, the delta axis on a
  comparison, the colours the server panel uses. The picture is checked as pixels: its size, its
  ground, that the trace's colour appears where the trace should be and nowhere it should not.
- **The store.** Put, get, replace on a second post, the recent laps of a driver and of everyone,
  pruning by age. Against a real PostgreSQL, in a schema of the test's own.
- **The routes.** Who may post, who may look, what each status code means, that a driver's page
  and chart page list their laps and nobody else's, and that the chart page shows the file for the
  laps it was given and refuses a lap that was never posted.
- **The manifest.** `plugin.json` validates against the contract, calls nothing outside the machine,
  and declares the routes the code serves.

It does not assert that a chart is pretty. `VT_SAMPLE_DIR=/somewhere go test -run TestWriteSampleChart .`
writes one to look at, with a comparison beside it.

## What a lap costs

`make bench`, on an M-series laptop, for a lap of 1 001 points:

| | |
|---|---|
| Reading an upload | about 1 ms |
| Drawing the document (SVG, ~80 KB) | about 0.6 ms, 200 allocations |
| Drawing the picture (PNG, 1400 × 650) | about 27 ms, 11 MB, 600 allocations |

A picture is drawn on request and not kept; the trace is what is stored, gzip-compressed JSON, about
19 KB a lap of 1 001 points.

## Against a running server

The try scripts at the root of the development checkout post modelled laps. With a paired device
(`try/engineer.sh pair`) and a stint (`try/engineer.sh stint practice`):

```
try/engineer.sh chart 2         # post lap 2, download its PNG as try/lap-2.png
try/engineer.sh chart 3 1.5     # a lap a second and a half slower, for a comparison
```

Then open `/plugin/visual-telemetry/me` as the driver, or `/plugin/visual-telemetry/` as an
administrator, tick both and overlay them. The modelled lap is a model: real pedal traces come from
the client.
