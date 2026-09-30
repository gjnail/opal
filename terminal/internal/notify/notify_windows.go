package notify

import (
	"encoding/base64"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

// AppID is the AppUserModelID Opal Terminal runs under. Toasts from an
// unpackaged app only show when a Start menu shortcut carries this ID; the
// installer creates one.
const AppID = "Opal.Terminal"

// powershellAppID is used when that shortcut doesn't exist: the toast then
// comes from Windows PowerShell, which Windows always allows.
const powershellAppID = `{1AC14E77-02E7-4E5D-B744-2EB1AE5198B7}\WindowsPowerShell\v1.0\powershell.exe`

var procSetAppID = windows.NewLazySystemDLL("shell32.dll").NewProc("SetCurrentProcessExplicitAppUserModelID")

// SetProcessAppID groups the app's windows under AppID on the taskbar, so
// pinning and the Start menu shortcut line up.
func SetProcessAppID() {
	if p, err := windows.UTF16PtrFromString(AppID); err == nil && procSetAppID.Find() == nil {
		procSetAppID.Call(uintptr(unsafe.Pointer(p)))
	}
}

// ShortcutPath is where the installer puts the Start menu shortcut.
func ShortcutPath() string {
	return filepath.Join(os.Getenv("APPDATA"), `Microsoft\Windows\Start Menu\Programs\Opal Terminal.lnk`)
}

// The toast is built with the WinRT API that Windows PowerShell 5.1 can
// reach. Title and body come in through the environment, never through
// the script text, so nothing in them can run as code.
const toastScript = `
[Windows.UI.Notifications.ToastNotificationManager, Windows.UI.Notifications, ContentType = WindowsRuntime] | Out-Null
[Windows.Data.Xml.Dom.XmlDocument, Windows.Data.Xml.Dom.XmlDocument, ContentType = WindowsRuntime] | Out-Null
$t = [Security.SecurityElement]::Escape($env:OPAL_NOTIFY_TITLE)
$b = [Security.SecurityElement]::Escape($env:OPAL_NOTIFY_BODY)
$xml = New-Object Windows.Data.Xml.Dom.XmlDocument
$xml.LoadXml("<toast><visual><binding template=""ToastGeneric""><text>$t</text><text>$b</text></binding></visual></toast>")
[Windows.UI.Notifications.ToastNotificationManager]::CreateToastNotifier($env:OPAL_NOTIFY_APPID).Show([Windows.UI.Notifications.ToastNotification]::new($xml))
`

// encodeScript is PowerShell's -EncodedCommand format: base64 of UTF-16LE.
func encodeScript(s string) string {
	u := utf16.Encode([]rune(s))
	b := make([]byte, 2*len(u))
	for i, c := range u {
		b[2*i], b[2*i+1] = byte(c), byte(c>>8)
	}
	return base64.StdEncoding.EncodeToString(b)
}

func send(title, body string) {
	appID := powershellAppID
	if _, err := os.Stat(ShortcutPath()); err == nil {
		appID = AppID
	}
	ps := filepath.Join(os.Getenv("SystemRoot"), `System32\WindowsPowerShell\v1.0\powershell.exe`)
	// -EncodedCommand sidesteps command-line quoting entirely.
	cmd := exec.Command(ps, "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-EncodedCommand", encodeScript(toastScript))
	cmd.Env = append(os.Environ(),
		"OPAL_NOTIFY_TITLE="+title,
		"OPAL_NOTIFY_BODY="+body,
		"OPAL_NOTIFY_APPID="+appID,
	)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW}
	cmd.Run()
}
