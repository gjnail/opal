module opal/terminal

go 1.26.0

require (
	github.com/BurntSushi/toml v1.6.0
	github.com/creack/pty v1.1.24
	github.com/go-text/typesetting v0.3.5
	github.com/klauspost/compress v1.20.1
	github.com/rivo/uniseg v0.4.7
	golang.org/x/image v0.26.0
	golang.org/x/sys v0.48.0
	opal v0.0.0-00010101000000-000000000000
)

require (
	gioui.org v0.10.3
	gioui.org/shader v1.0.9 // indirect
	github.com/godbus/dbus/v5 v5.2.2
	golang.org/x/exp/shiny v0.0.0-20250408133849-7e4ce0ab07d0 // indirect
	golang.org/x/net v0.48.0 // indirect
	golang.org/x/text v0.32.0 // indirect
)

replace opal => ../

replace gioui.org => ./third_party/gio
