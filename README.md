# RenameLite

A small batch rename tool. Add files, pick a rule, see every new name
before anything touches the disk. Built with Go and Wails; the front end
is plain HTML, CSS, and JavaScript embedded in the binary.

## Rules

Four rule pages, one active at a time:

- **Whole** — build the name from a template. `*` inserts the original
  name, `#` inserts a serial number or letter (`a, b, c...`). Start,
  step, digit width, zero padding, and extension are configurable.
- **Replace** — replace every occurrence of a string in the name.
- **Add/Delete** — prefix, suffix, insert at a position, delete a
  string, or delete a character range. Acts on the name without the
  extension.
- **Regex** — replace a regular expression match; `$1` and `${name}`
  group references work in the replacement. The expression is checked
  as you type.

Every page can lowercase or uppercase the name, the extension, or both.
On a name conflict you choose: ask, overwrite, skip, or auto-rename
(appends `(2)`, `(3)`, ...).

## Build

Requires Go, Node (the front end is TypeScript, compiled in place by
`tsc` — no bundler, no runtime dependencies), and the Wails CLI:

```
go install github.com/wailsapp/wails/v2/cmd/wails@latest
wails build
```

`wails build` runs `npm ci` and `tsc` through the hooks in
`wails.json`. The binary lands in `build/bin/renamelite(.exe)`. Platform
notes: Wails builds natively per platform (WebView2 on Windows,
WebKitGTK on Linux, Cocoa/WebKit on macOS), so each target is built on
its own OS. The window sizes itself to its content at startup, so the
layout adapts to each platform's font metrics.

## Releases

Push a tag named `v*` and the Release workflow builds all three
platforms and publishes archives to the GitHub Release (CI on `main`
runs vet, gofmt, race tests, and a coverage gate on Linux + Windows):

```
git tag v1.0.0 && git push origin v1.0.0
```

Releases are not code-signed or notarized, so first launch shows a
SmartScreen/Quarantine prompt.

## Test

The rename engine is pure logic and tests run without building the GUI:

```
go test ./...
```

## Layout

- `internal/rename` — the rename engine. Pure functions, no file system.
- `app.go` / `main.go` — Wails bindings, list state, and the rename run.
- `frontend/src` — the window: HTML, CSS, and a thin JS layer that only
  moves form state to Go and table rows back.

## License

MIT. See `LICENSE`.
