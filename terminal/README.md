# Opal Terminal

A terminal emulator for opal, written in Go on [Gio](https://gioui.org). It
reads opal's `config.toml`, takes its colors from the opal theme, and uses the
command marks opal's prompt emits. The main [README](../README.md#opal-terminal)
describes what it does; this file covers building, configuration and the
source.

It's developed and used on Windows. The macOS and Linux code builds and passes
its tests in CI, but hasn't been run on those systems yet, and there are no
release builds.

This is a separate Go module so the `opal` CLI keeps building with Go 1.22;
this one needs Go 1.26.

## Building

```sh
cd terminal
go build -o opal-terminal .           # macOS and Linux need cgo; see Gio's docs for Linux packages
go build -ldflags=-H=windowsgui -o opal-terminal.exe .   # Windows, without a console window
```

`opal-terminal -list-profiles` shows the shells it found; `-profile NAME`
starts one and `-dir PATH` starts in a directory; anything after the flags
runs instead of a shell (`opal-terminal htop`).

On Windows, run `scripts/fetch-conpty.ps1` to put Microsoft's newer ConPTY
(`conpty.dll` and `OpenConsole.exe`, from the Microsoft.Windows.Console.ConPTY
package) next to the executable. The ConPTY built into Windows drops the
escape sequences for sixel and kitty images; with the newer one present,
Opal Terminal uses it automatically (`OPAL_TERMINAL_CONPTY=system` turns that
off).

## Configuration

Ctrl+Comma (Cmd+Comma on macOS) opens the settings page, which edits the
values below and applies them as you change them; `opal-terminal -settings`
opens it at launch. Everything is saved in the `[terminal]` table of Opal's
`config.toml`, which you can also edit by hand (`opal config edit`):

```toml
[terminal]
font_family = ["JetBrains Mono", "Cascadia Mono"]
font_size = 12            # points (13 on macOS)
line_height = 1.0
cell_width = 1.0          # widens or narrows every cell
shell = "PowerShell"      # a profile name, or a command line
scrollback = 10000
grapheme_clustering = true  # treat emoji sequences and combining marks as one character
cursor_style = "block"    # block, bar, underline
cursor_blink = true
copy_on_select = false
word_chars = "-_./~:@+%#?&="  # count as part of a word when double-clicking
bold_is_bright = false
min_contrast = 1.0        # e.g. 4.5 lifts low-contrast text
padding = 8
bell = "visual"           # visual, sound, none
notify_after = 10         # seconds; background commands longer than this get a note
notifications = "unfocused"  # desktop notifications: unfocused, always, never
restore_session = true    # reopen the last window's tabs on launch
# theme = "boulder"       # use a different theme than the prompt
# background = "light"    # and a different variant of it

[terminal.clipboard]
write = true              # programs may set the clipboard (OSC 52)
read = false              # programs may read it

[terminal.colors]         # overrides for the theme-derived colors
# background = "#101014"
# palette = ["#000000", "#ff5555"]   # up to 16 entries

[[terminal.profiles]]
name = "work box"
command = "ssh"
args = ["me@work"]

[terminal.keys]
"ctrl+shift+t" = "new_tab"
"ctrl+alt+k" = "clear_scrollback"
"f11" = "none"            # unbind a default
```

Default keys (macOS uses Cmd in place of Ctrl+Shift):

| | |
|---|---|
| Ctrl+Shift+T / W / N | new tab / close pane / new window |
| Ctrl+Tab, Ctrl+Shift+Tab | next / previous tab |
| Alt+Shift+Plus / Minus / D | split right / down / along the longer side |
| Alt+arrows, Alt+Shift+arrows | focus / resize panes |
| Ctrl+Shift+Z | zoom the pane |
| Ctrl+Shift+C / V, Ctrl+V on Windows | copy / paste (Ctrl+C copies when text is selected) |
| Ctrl+Shift+F | find |
| Ctrl+Shift+Space | quick select |
| Ctrl+Shift+P | command palette |
| Ctrl+Up / Down | previous / next prompt |
| Ctrl+Shift+E | select the last command's output |
| Ctrl+Plus / Minus / 0 | font size |
| Ctrl+Shift+K | clear scrollback |
| Ctrl+Shift+H / J | search command history / open a recent directory |
| Shift+Insert, Ctrl+Insert | paste, copy |
| Ctrl+Comma | settings |

## Layout

- `internal/vt`: the parser and screen model.
- `internal/pty`: runs the shell on a pseudo terminal: creack/pty on macOS and
  Linux, ConPTY on Windows.
- `internal/fonts`: font discovery, fallback and glyph rasterization.
- `internal/render`: draws one terminal row into an image. Rows are cached by
  content, so scrolling only redraws new lines. Box drawing, block elements,
  braille and powerline separators are drawn from geometry so they tile
  without gaps.
- `internal/settings`: the `[terminal]` config, theme colors and shell
  detection.
- `internal/ui`: windows, tabs, panes, input and the chrome, on Gio.

## Testing

```sh
cd terminal
go test ./...
```

The PTY tests start real processes (`cmd.exe` and PowerShell on Windows, `sh`
elsewhere). Set `OPAL_DUMP=out.png` when running the `fonts` or `render` tests
to write a sample image. `OPAL_TERMINAL_TRACE=path` makes the app write each
pane's raw output to `path.N`, and `OPAL_TERMINAL_DEBUG=path` logs input
events.

## Third-party files

`internal/fonts/embedded` holds Symbols Nerd Font Mono from the
[Nerd Fonts](https://github.com/ryanoasis/nerd-fonts) project. See
`SymbolsNerdFont-LICENSE.txt` and `SymbolsNerdFont-README.md` there for the
licenses of the bundled icon sets.

`third_party/gio` is Gio v0.10.3 with a few input fixes (the Insert key, and
emoji typed on Windows); `third_party/gio/PATCHES.md` lists them.
