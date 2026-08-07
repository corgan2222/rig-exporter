//go:build windows

package sysinfo

import (
	"strings"

	"golang.org/x/sys/windows/registry"
)

// firmwareKey is where Windows publishes the SMBIOS system and BIOS identity.
//
// The same strings live in the SMBIOS tables this project already parses for
// the memory modules, but reaching them there would mean carrying a second
// structure walker into a package that has nothing to do with memory. Windows
// has already done the parsing; reading the result costs one registry open.
const firmwareKey = `HARDWARE\DESCRIPTION\System\BIOS`

// Hypervisor names the virtualisation platform this machine runs on, or an
// empty string when the firmware looks like real hardware.
//
// Read once and kept: a machine does not move between real and virtual
// hardware while the process is running.
func (p *Provider) Hypervisor() string {
	p.hypervisorOnce.Do(func() {
		p.hypervisor = IdentifyHypervisor(firmwareIdentity())
	})
	return p.hypervisor
}

// firmwareIdentity reads the three strings the detection is based on. A missing
// key or value yields an empty string, which IdentifyHypervisor reads as "no
// signature" rather than as an error: firmware that says nothing is a normal
// state, not a failure.
func firmwareIdentity() (manufacturer, product, biosVendor string) {
	key, err := registry.OpenKey(registry.LOCAL_MACHINE, firmwareKey, registry.QUERY_VALUE)
	if err != nil {
		return "", "", ""
	}
	defer key.Close()

	read := func(name string) string {
		value, _, err := key.GetStringValue(name)
		if err != nil {
			return ""
		}
		return strings.TrimSpace(value)
	}
	return read("SystemManufacturer"), read("SystemProductName"), read("BIOSVendor")
}
