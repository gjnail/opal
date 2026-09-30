# Local patches

This is Gio v0.10.3 (https://gioui.org, dual licensed under the Unlicense
and MIT; see LICENSE) with small changes Opal Terminal needs. Tests,
testdata and the Nix files were left out. Every change is marked with an
`opal-terminal patch` comment.

- `io/key`: a `NameInsert` key name. Gio had no name for Insert, so
  Shift+Insert and Ctrl+Insert never reached the application.
- `app/os_windows.go`, `app/internal/windows`: map `VK_INSERT`; join the
  UTF-16 surrogate pairs that `WM_CHAR` delivers in two messages, so emoji
  typed with the emoji panel arrive, and accept format characters such as
  the zero-width joiner that emoji sequences use.
- `app/os_macos.go`: map the Insert/Help function keys.
- `app/internal/xkb`: map Insert and keypad Insert on Linux.

To update Gio: copy the new release here, drop its tests, and reapply the
changes above.
