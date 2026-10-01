package ui

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework AppKit

#import <AppKit/AppKit.h>

// AppKit wants both on the main thread; the window's event loop isn't it.
static void opalRequestAttention(void) {
	dispatch_async(dispatch_get_main_queue(), ^{
		[NSApp requestUserAttention:NSInformationalRequest];
	});
}

static void opalBeep(void) {
	dispatch_async(dispatch_get_main_queue(), ^{
		NSBeep();
	});
}
*/
import "C"

func (w *Window) nativeWindow(e any) {}

// requestAttention bounces the Dock icon once. macOS only does it while
// another app is in front.
func (w *Window) requestAttention() {
	if w.focused {
		return
	}
	C.opalRequestAttention()
}

// beep plays the alert sound chosen in System Settings, for bell = "sound".
func (w *Window) beep() { C.opalBeep() }
