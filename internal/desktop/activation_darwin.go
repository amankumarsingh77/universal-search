//go:build darwin

package desktop

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Cocoa
#import <Cocoa/Cocoa.h>

static void setAccessoryActivationPolicy() {
    dispatch_async(dispatch_get_main_queue(), ^{
        [NSApp setActivationPolicy:NSApplicationActivationPolicyAccessory];
    });
}
*/
import "C"

// SetAccessoryActivationPolicy switches the running macOS app to the
// "menu bar accessory" activation policy. The app stops appearing in the
// Dock and Cmd-Tab switcher, matching Raycast / Alfred behavior. Wails
// hardcodes Regular policy in its AppDelegate, so this must be called
// after Wails startup to take effect.
func SetAccessoryActivationPolicy() {
	C.setAccessoryActivationPolicy()
}
