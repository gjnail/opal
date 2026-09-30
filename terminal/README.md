# Opal Terminal (work in progress)

A terminal emulator for Opal, written in Go. It is a separate module so the
`opal` CLI keeps building with Go 1.22; this module needs Go 1.26 because the
window layer will use [Gio](https://gioui.org).

The emulation engine, the process layer, font handling and the row renderer
work and have tests. There is no window yet, so this is not usable as a
terminal today.

## Layout

- `internal/vt`: the parser and screen model. xterm-compatible control
  sequences, scrollback that reflows on resize, grapheme clusters (mode 2027),
  true color and styled underlines, OSC 8 hyperlinks, OSC 52 clipboard,
  OSC 133/633 shell integration, the kitty keyboard protocol flag stack, and
  DECRQSS/XTGETTCAP queries.
- `internal/pty`: runs the shell on a pseudo terminal: creack/pty on macOS and
  Linux, ConPTY on Windows.
- `internal/fonts`: finds the monospace font with per-character fallback,
  draws color emoji (COLR v0/v1 and PNG strikes), and bundles the Nerd Font
  symbols that Opal's prompt uses.
- `internal/render`: draws one terminal row into an RGBA image. Box drawing,
  block elements, braille and powerline separators are drawn from geometry so
  they tile without gaps.

## Testing

```sh
cd terminal
go test ./...
```

The PTY tests start real processes (`cmd.exe` and PowerShell on Windows, `sh`
elsewhere). Set `OPAL_DUMP=out.png` when running the `fonts` or `render` tests
to write a sample image.

## Third-party files

`internal/fonts/embedded` holds Symbols Nerd Font Mono from the
[Nerd Fonts](https://github.com/ryanoasis/nerd-fonts) project. See
`SymbolsNerdFont-LICENSE.txt` and `SymbolsNerdFont-README.md` there for the
licenses of the bundled icon sets.
