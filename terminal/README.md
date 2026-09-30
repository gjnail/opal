# Opal Terminal (work in progress)

A terminal emulator for Opal, written in Go. It is a separate module so the
`opal` CLI keeps building with Go 1.22; this module needs Go 1.26 because the
window layer uses [Gio](https://gioui.org).

It runs on Windows today (built and used there during development). The macOS
and Linux code compiles but hasn't been run on those systems yet, and there
are no release builds.

## Building

```sh
cd terminal
go build -o opal-terminal .           # macOS and Linux need cgo; see Gio's docs for Linux packages
go build -ldflags=-H=windowsgui -o opal-terminal.exe .   # Windows, without a console window
```

`opal-terminal -list-profiles` shows the shells it found; `-profile NAME`
starts one; anything after the flags runs instead of a shell
(`opal-terminal htop`).

On Windows, run `scripts/fetch-conpty.ps1` to put Microsoft's newer ConPTY
(`conpty.dll` and `OpenConsole.exe`, from the Microsoft.Windows.Console.ConPTY
package) next to the executable. The ConPTY built into Windows drops the
escape sequences for sixel and kitty images; with the newer one present,
Opal Terminal uses it automatically (`OPAL_TERMINAL_CONPTY=system` turns that
off).

## What works

- Tabs (drag to reorder, middle-click to close) and split panes with
  draggable dividers, zoom, and keyboard focus and resize.
- A command palette listing every action with its keybinding.
- Find in scrollback, with case and regex toggles.
- Quick select: every URL, path, git hash, IP address and UUID on screen gets
  a one- or two-letter label; typing it copies the match (uppercase also
  pastes it at the prompt).
- Selection by character, word (double-click), line (triple-click) and block
  (Alt+drag); copy on select is optional. Ctrl+click opens links, both
  OSC 8 hyperlinks and plain URLs in the text.
- Shell integration through the OSC 133 marks Opal's prompt emits: a gutter
  mark per command colored by exit status, jumping between prompts, selecting
  or copying the last command's output, and a note when a long command
  finishes in a background tab.
- Colors follow the Opal theme in `config.toml`, so the terminal matches the
  prompt. Font fallback covers CJK, color emoji and the Nerd Font icons the
  prompt uses (bundled, so nothing needs installing).
- xterm-compatible emulation: scrollback that reflows on resize, true color,
  curly and colored underlines, grapheme clusters, bracketed paste, focus
  events, mouse reporting, the kitty keyboard protocol, synchronized output,
  OSC 52 clipboard (writing allowed, reading off by default), notifications
  and progress reports.

- Inline images through all three protocols programs use: sixel, the kitty
  graphics protocol (including chunked, compressed and file transfers,
  placements and deletion) and iTerm2's `File=` sequence. Images scroll and
  reflow with the text they sit on.
- Desktop notifications for OSC 9/777/99 and for long commands that finish
  while you're elsewhere: toasts on Windows, Notification Center on macOS,
  the freedesktop service on Linux.
- Session restore: the tabs, splits, shells, working directories and recent
  output of the last window you closed come back on the next launch.

Not done yet: kitty's Unicode placeholders and animation.

## Configuration

Settings live in the `[terminal]` table of Opal's `config.toml`
(`opal config edit`, or Ctrl+Comma in the terminal):

```toml
[terminal]
font_family = ["JetBrains Mono", "Cascadia Mono"]
font_size = 12            # points
line_height = 1.0
shell = "PowerShell"      # a profile name, or a command line
scrollback = 10000
cursor_style = "block"    # block, bar, underline
cursor_blink = true
copy_on_select = false
bold_is_bright = false
min_contrast = 1.0        # e.g. 4.5 lifts low-contrast text
padding = 8
bell = "visual"           # visual, sound, none
notify_after = 10         # seconds; background commands longer than this get a note
notifications = "unfocused"  # desktop notifications: unfocused, always, never
restore_session = true    # reopen the last window's tabs on launch

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
