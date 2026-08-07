package sysinfo

import "strings"

// IdentifyHypervisor names the virtualisation platform from the identity the
// firmware publishes, or returns an empty string when nothing says "guest".
//
// The firmware identity answers this, deliberately not the processor's
// hypervisor bit. Windows sets that bit on bare metal as well, the moment
// Hyper-V, WSL 2 or memory integrity is switched on — each of those puts the
// host itself on top of a hypervisor, and a gaming PC with virtualisation-based
// security enabled would report itself as a virtual machine. A board whose
// manufacturer is "QEMU", on the other hand, is a guest and nothing else.
//
// A negative answer is worth less than a positive one, and the caller should
// treat it that way: a hypervisor can be configured to pass the host board's
// identity straight through, and then there is nothing here to see. "No known
// signature" is the honest reading; "definitely real hardware" is not.
//
// The names are values that get published, so they stay in English and stay
// stable — a dashboard filtering on them must not break because the interface
// language changed.
func IdentifyHypervisor(manufacturer, product, biosVendor string) string {
	haystack := strings.ToLower(strings.Join(
		[]string{manufacturer, product, biosVendor}, "\x00"))

	for _, signature := range hypervisorSignatures {
		if strings.Contains(haystack, signature.needle) {
			return signature.name
		}
	}
	return ""
}

// hypervisorSignatures is ordered from the most specific marker to the
// loosest, because the first match wins. "Standard PC" and "Virtual Machine"
// sit at the end for that reason: they are product names rather than vendor
// names, and a vendor that names itself outright should be believed first.
var hypervisorSignatures = []struct {
	needle string
	name   string
}{
	{"vmware", "VMware"},
	// VirtualBox names its manufacturer after the company that wrote it, and
	// only the product after the product.
	{"innotek", "VirtualBox"},
	{"virtualbox", "VirtualBox"},
	{"parallels", "Parallels"},
	{"amazon ec2", "Amazon EC2"},
	{"google compute engine", "Google Compute Engine"},
	{"alibaba cloud", "Alibaba Cloud"},
	{"openstack", "OpenStack"},
	{"qemu", "QEMU/KVM"},
	// The firmware QEMU ships identifies itself as Bochs, and Red Hat is what
	// the virtio devices of a KVM guest are signed with.
	{"bochs", "QEMU/KVM"},
	{"red hat", "QEMU/KVM"},
	{"kvm", "QEMU/KVM"},
	{"bhyve", "bhyve"},
	{"hvm domu", "Xen"},
	{"xen", "Xen"},
	// Hyper-V leaves the manufacturer as Microsoft, which a Surface would too;
	// the product name is what separates the two.
	{"virtual machine", "Hyper-V"},
	// QEMU's machine types, for a guest whose manufacturer was overridden but
	// whose chipset model was not.
	{"standard pc", "QEMU/KVM"},
}
