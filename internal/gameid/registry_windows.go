package gameid

import "golang.org/x/sys/windows/registry"

// CurrentUser reads HKCU, which is where Steam keeps both values. They belong
// to the logged-in user rather than the machine, so HKLM would be the wrong
// hive and an empty one.
type CurrentUser struct{}

func (CurrentUser) open(path string) (registry.Key, bool) {
	k, err := registry.OpenKey(registry.CURRENT_USER, path, registry.QUERY_VALUE)
	if err != nil {
		return 0, false
	}
	return k, true
}

// String reads a string value, reporting false for anything missing.
func (c CurrentUser) String(path, name string) (string, bool) {
	k, ok := c.open(path)
	if !ok {
		return "", false
	}
	defer k.Close()

	v, _, err := k.GetStringValue(name)
	if err != nil {
		return "", false
	}
	return v, true
}

// Uint reads a numeric value, reporting false for anything missing or of
// another type.
func (c CurrentUser) Uint(path, name string) (uint64, bool) {
	k, ok := c.open(path)
	if !ok {
		return 0, false
	}
	defer k.Close()

	v, _, err := k.GetIntegerValue(name)
	if err != nil {
		return 0, false
	}
	return v, true
}
