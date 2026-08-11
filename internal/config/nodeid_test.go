package config

import "testing"

// The node id is in every entity id on this machine.
//
// "pc" typed into the form is a choice. Slug("") happens to return the same
// string, and after the slug the two are the same three letters — so a user who
// deliberately typed pc had it replaced by their display name, and every
// ObjectID, UniqueID and DeviceIdentifier moved with it. Normalize runs on
// every Load and every Save, so it did not need a first start to happen.
func TestNormalizeKeepsAnExplicitlyTypedNodeID(t *testing.T) {
	cfg := Defaults()
	cfg.DeviceName = "Wohnzimmer-PC"
	cfg.NodeID = "pc"
	cfg.Normalize()

	if cfg.NodeID != "pc" {
		t.Errorf("NodeID = %q, want %q", cfg.NodeID, "pc")
	}
}

// Renaming what is displayed must not move the identifiers underneath it.
// Somebody relabelling their PC in Home Assistant is not asking for every
// entity on it to be replaced by a new one.
func TestRenamingTheDeviceLeavesTheNodeIDAlone(t *testing.T) {
	cfg := Defaults()
	cfg.DeviceName = "PC"
	cfg.NodeID = "pc"
	cfg.Normalize()

	cfg.DeviceName = "Wohnzimmer-PC" // display only
	cfg.Normalize()

	if cfg.NodeID != "pc" {
		t.Errorf("NodeID = %q after a display rename, want %q — every entity id "+
			"on this machine just moved", cfg.NodeID, "pc")
	}
}

// The fallback has to survive: an unset node id still comes from the device
// name. This one is green before the fix and is here so it stays that way.
func TestAnUnsetNodeIDComesFromTheDeviceName(t *testing.T) {
	cfg := Defaults()
	cfg.DeviceName = "Wohnzimmer-PC"
	cfg.NodeID = ""
	cfg.Normalize()

	if cfg.NodeID != "wohnzimmer_pc" {
		t.Errorf("NodeID = %q, want %q", cfg.NodeID, "wohnzimmer_pc")
	}
}

// Whitespace is not a value. A node id of spaces is unset, not a choice.
func TestANodeIDOfNothingButSpacesCountsAsUnset(t *testing.T) {
	cfg := Defaults()
	cfg.DeviceName = "Wohnzimmer-PC"
	cfg.NodeID = "   "
	cfg.Normalize()

	if cfg.NodeID != "wohnzimmer_pc" {
		t.Errorf("NodeID = %q, want it derived from the device name", cfg.NodeID)
	}
}

// And when neither is usable there still has to be something, because an empty
// node id would produce entity ids that start with a separator.
func TestSomethingUsableIsAlwaysLeft(t *testing.T) {
	cfg := Defaults()
	cfg.DeviceName = "***"
	cfg.NodeID = ""
	cfg.Normalize()

	if cfg.NodeID != "pc" {
		t.Errorf("NodeID = %q, want the fallback %q", cfg.NodeID, "pc")
	}
}
