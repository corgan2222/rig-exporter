//go:build windows

package webui

import (
	"testing"

	"github.com/corgan2222/rig-exporter/internal/collector"
	"github.com/corgan2222/rig-exporter/internal/hardware/gpu"
	"github.com/corgan2222/rig-exporter/internal/metrics"
)

// The dashboard banner asks for a different thing in each of these three
// situations, so telling them apart is the whole job of amdPresence.
func TestAMDPresenceSeparatesTheThreeCasesTheBannerNeeds(t *testing.T) {
	for _, tc := range []struct {
		name         string
		build        func(*metrics.Set)
		card, driver bool
	}{
		{
			name: "an NVIDIA machine is neither",
			build: func(set *metrics.Set) {
				set.Origin = "Windows DXGI"
				set.Add(metrics.Text(metrics.GPUVendor, "0", "NVIDIA"))
				set.Origin = "NVIDIA NVML"
				set.Add(metrics.Gauge(metrics.GPUTemperature, "0", 48))
			},
		},
		{
			// The display driver on its own: Windows knows the card, nothing
			// reports its temperature. This is the case that gets the pointer
			// at the full AMD package.
			name: "a Radeon whose driver says nothing is a card without a driver",
			build: func(set *metrics.Set) {
				set.Origin = "Windows DXGI"
				set.Add(
					metrics.Text(metrics.GPUName, "0", "Radeon RX 570 Series"),
					metrics.Text(metrics.GPUVendor, "0", "AMD"),
				)
			},
			card: true,
		},
		{
			name: "a Radeon reporting through ADLX is both",
			build: func(set *metrics.Set) {
				set.Origin = "Windows DXGI"
				set.Add(metrics.Text(metrics.GPUVendor, "0", "AMD"))
				set.Origin = gpu.ADLXOrigin
				set.Add(metrics.Gauge(metrics.GPUTemperature, "0", 55))
			},
			card:   true,
			driver: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			set := &metrics.Set{}
			tc.build(set)

			card, driver := amdPresence(collector.Snapshot{Set: *set})
			if card != tc.card || driver != tc.driver {
				t.Errorf("amdPresence = (%t, %t), want (%t, %t)",
					card, driver, tc.card, tc.driver)
			}
		})
	}
}

// Vendor strings reach this from DXGI, from Afterburner and from a card name,
// and nothing guarantees they agree on capitalisation.
func TestAMDPresenceIgnoresVendorCapitalisation(t *testing.T) {
	set := &metrics.Set{}
	set.Add(metrics.Text(metrics.GPUVendor, "0", "amd"))

	if card, _ := amdPresence(collector.Snapshot{Set: *set}); !card {
		t.Error("a lower-case vendor was not recognised as AMD")
	}
}

// An empty snapshot is the state before the first measurement lands. It has to
// answer no rather than reach into a reading that is not there.
func TestAMDPresenceOnAnEmptySnapshotClaimsNothing(t *testing.T) {
	card, driver := amdPresence(collector.Snapshot{})
	if card || driver {
		t.Errorf("amdPresence = (%t, %t) on an empty snapshot, want both false", card, driver)
	}
}
