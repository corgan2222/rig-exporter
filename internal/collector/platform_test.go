package collector

import (
	"testing"

	"github.com/corgan2222/rig-exporter/internal/metrics"
)

// A guest publishes both readings: the flag for automations, the name for
// whoever is looking at a value and wondering why the fan speed is missing.
func TestAGuestReportsBothTheFlagAndTheName(t *testing.T) {
	system := newSystem()
	system.hypervisor = "QEMU/KVM"

	got := newCollector(fakeRTSS{}, system).Collect()

	if !got.Flag(metrics.Virtualized.ID) {
		t.Error("virtualized is false on a machine that named a hypervisor")
	}
	if name := got.Str(metrics.Hypervisor.ID); name != "QEMU/KVM" {
		t.Errorf("hypervisor = %q, want QEMU/KVM", name)
	}
}

// Real hardware still answers the question — false is a reading, not a gap —
// but it names no hypervisor, because there is none to name.
func TestRealHardwareAnswersFalseAndNamesNothing(t *testing.T) {
	got := newCollector(fakeRTSS{}, newSystem()).Collect()

	if !got.Has(metrics.Virtualized.ID) {
		t.Fatal("virtualized was left out entirely")
	}
	if got.Flag(metrics.Virtualized.ID) {
		t.Error("virtualized is true although no hypervisor was named")
	}
	if got.Has(metrics.Hypervisor.ID) {
		t.Error("an empty hypervisor name was published")
	}
}

// The firmware cannot change under a running process, so it is asked once.
// Reading it every second would be a registry open per collection for an
// answer that is fixed at boot.
func TestTheFirmwareIsAskedOnlyOnce(t *testing.T) {
	system := newSystem()
	system.hypervisor = "VMware"

	c := newCollector(fakeRTSS{}, system)
	c.Collect()
	// A provider that changed its mind would prove the value was re-read.
	system.hypervisor = "Xen"

	if name := c.Collect().Str(metrics.Hypervisor.ID); name != "VMware" {
		t.Errorf("hypervisor = %q, want the first answer VMware", name)
	}
}
