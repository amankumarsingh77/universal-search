//go:build !darwin

package desktop

// SetAccessoryActivationPolicy is a no-op on non-macOS platforms. On macOS
// it switches the app to the "menu bar accessory" activation policy so it
// disappears from the Dock and Cmd-Tab.
func SetAccessoryActivationPolicy() {}
