package sysinfo

import "testing"

// The guest signatures are what these platforms actually publish. The QEMU row
// is the one that was read off a running Unraid guest on 07.08.2026; the rest
// are the identities their documentation and firmware are known to carry.
func TestKnownGuestsAreIdentified(t *testing.T) {
	for _, tc := range []struct {
		manufacturer, product, bios string
		want                        string
	}{
		{"QEMU", "Standard PC (i440FX + PIIX, 1996)", "EDK II", "QEMU/KVM"},
		{"QEMU", "Standard PC (Q35 + ICH9, 2009)", "SeaBIOS", "QEMU/KVM"},
		{"VMware, Inc.", "VMware Virtual Platform", "Phoenix Technologies LTD", "VMware"},
		{"VMware, Inc.", "VMware7,1", "VMware, Inc.", "VMware"},
		{"innotek GmbH", "VirtualBox", "innotek GmbH", "VirtualBox"},
		{"Microsoft Corporation", "Virtual Machine", "American Megatrends Inc.", "Hyper-V"},
		{"Xen", "HVM domU", "Xen", "Xen"},
		{"Parallels Software International Inc.", "Parallels Virtual Platform", "", "Parallels"},
		{"Amazon EC2", "t3.medium", "Amazon EC2", "Amazon EC2"},
		{"Google", "Google Compute Engine", "Google", "Google Compute Engine"},
		// A guest whose manufacturer was overridden but whose chipset model
		// still gives it away.
		{"Contoso Ltd.", "Standard PC (Q35 + ICH9, 2009)", "SeaBIOS", "QEMU/KVM"},
	} {
		t.Run(tc.want+" "+tc.product, func(t *testing.T) {
			if got := IdentifyHypervisor(tc.manufacturer, tc.product, tc.bios); got != tc.want {
				t.Errorf("IdentifyHypervisor(%q, %q, %q) = %q, want %q",
					tc.manufacturer, tc.product, tc.bios, got, tc.want)
			}
		})
	}
}

// A false positive is the expensive mistake here: it would tell somebody their
// gaming PC is a virtual machine, and send them looking for a fault in the one
// reading that is actually right.
func TestRealHardwareIsNotMistakenForAGuest(t *testing.T) {
	for _, tc := range []struct{ manufacturer, product, bios string }{
		{"ASUSTeK COMPUTER INC.", "ROG STRIX X570-E GAMING", "American Megatrends Inc."},
		{"Micro-Star International Co., Ltd.", "MS-7C37", "American Megatrends International, LLC."},
		{"Gigabyte Technology Co., Ltd.", "X570 AORUS ELITE", "American Megatrends Inc."},
		{"Dell Inc.", "XPS 15 9500", "Dell Inc."},
		{"LENOVO", "20XW00AYGE", "LENOVO"},
		{"Intel Corporation", "NUC12WSHi7", "Intel Corp."},
		// The one that makes the Hyper-V rule key on the product rather than on
		// the manufacturer: Microsoft builds real machines too.
		{"Microsoft Corporation", "Surface Laptop 4", "Microsoft Corporation"},
		// Firmware that says nothing at all is not evidence of anything.
		{"", "", ""},
		{"To Be Filled By O.E.M.", "To Be Filled By O.E.M.", "American Megatrends Inc."},
	} {
		t.Run(tc.manufacturer+" "+tc.product, func(t *testing.T) {
			if got := IdentifyHypervisor(tc.manufacturer, tc.product, tc.bios); got != "" {
				t.Errorf("IdentifyHypervisor(%q, %q, %q) = %q, want no hypervisor",
					tc.manufacturer, tc.product, tc.bios, got)
			}
		})
	}
}

// Firmware capitalises how it likes, and the same platform is spelled
// differently by different releases.
func TestIdentificationIgnoresCapitalisation(t *testing.T) {
	if got := IdentifyHypervisor("qemu", "standard pc", "seabios"); got != "QEMU/KVM" {
		t.Errorf("IdentifyHypervisor = %q, want QEMU/KVM", got)
	}
	if got := IdentifyHypervisor("VMWARE, INC.", "VMWARE VIRTUAL PLATFORM", ""); got != "VMware" {
		t.Errorf("IdentifyHypervisor = %q, want VMware", got)
	}
}

// The three fields are searched as separate strings, so a needle must not be
// formed by one field running into the next.
func TestASignatureIsNotAssembledAcrossFields(t *testing.T) {
	// "…innote" plus "k GmbH…" would spell innotek if the fields were simply
	// concatenated.
	if got := IdentifyHypervisor("Acme innote", "k GmbH board", ""); got != "" {
		t.Errorf("IdentifyHypervisor = %q, want no hypervisor", got)
	}
}
