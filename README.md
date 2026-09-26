# Hrename

Hrename is a batch file rename tool for Windows, written in Rust. You add
files to a list, pick one rename rule, and see the new name of each file
before you apply it.

## Rules

The tool has four rule pages:

- Pattern. Build a new name from a template. `*` inserts the original name.
  `#` inserts a serial number. You set the start value, the step, the digit
  count, zero padding, and letter numbering (`a, b, c` instead of `1, 2, 3`).
- Replace. Replace one string in the file name with another string.
- Add or delete. Add a prefix, add a suffix, insert text at a position, delete
  a string, or delete a range of characters.
- Regex. Replace a regular expression match. The replacement can use `$1` and
  `${name}` capture group references. The tool checks the expression as you
  type and shows errors in red.

Each page can change the case of the name, the extension, or both. When a new
name already exists, you pick one policy: ask, overwrite, skip, or rename the
new file automatically.

## Build

You need a Rust toolchain from rustup.rs.

```
cargo build -p hrename --release
```

The executable is `target/release/hrename.exe`.

## Test

The engine tests are fast because they do not build the GUI.

```
cargo test -p hrename-core
```

## Layout

- `crates/core` — the rename engine as a library, with unit tests.
- `crates/gui` — the egui window on top of the engine.

## License

MIT. See `LICENSE`.
