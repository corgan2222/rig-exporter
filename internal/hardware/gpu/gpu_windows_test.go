//go:build windows

package gpu

import (
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/corgan2222/rig-exporter/internal/hardware/afterburner"
	"github.com/corgan2222/rig-exporter/internal/metrics"
)

const mebibyte = 1024 * 1024

func card(index int, name string) nvmlCard {
	return nvmlCard{Index: index, Name: name}
}

// Two of the same card is the case that broke: names matched against a map, and
// Go randomises map iteration, so both cards could claim instance 0 — one card
// got the other's VRAM and power limit, the other got nothing, and which was
// which changed between runs.
func TestIdenticalCardsKeepSeparateInstances(t *testing.T) {
	cards := map[string]string{"0": "NVIDIA GeForce RTX 4090", "1": "NVIDIA GeForce RTX 4090"}
	nvml := []nvmlCard{card(0, "NVIDIA GeForce RTX 4090"), card(1, "NVIDIA GeForce RTX 4090")}

	// Repeated because the failure it guards against was intermittent.
	for i := 0; i < 50; i++ {
		got := assignInstances(nvml, cards)
		if got[0] != "0" || got[1] != "1" {
			t.Fatalf("assignInstances = %v, want [0 1] every time", got)
		}
	}
}

// A laptop: Afterburner sees the integrated chip first. If NVML spells the
// discrete card differently, it must not be written over the integrated one.
func TestAnUnmatchedCardNeverTakesANamedInstance(t *testing.T) {
	cards := map[string]string{"0": "Intel UHD Graphics 770", "1": "NVIDIA RTX 4070 Laptop GPU"}
	nvml := []nvmlCard{card(0, "NVIDIA GeForce RTX 4070 Laptop GPU")}

	got := assignInstances(nvml, cards)
	if len(got) != 1 {
		t.Fatalf("got %d instances, want 1", len(got))
	}
	if _, collides := cards[got[0]]; collides {
		t.Errorf("instance %q belongs to %q already", got[0], cards[got[0]])
	}
}

func TestNamesMatchRegardlessOfCaseAndPadding(t *testing.T) {
	cards := map[string]string{"0": "  NVIDIA GeForce RTX 2080  "}
	got := assignInstances([]nvmlCard{card(0, "nvidia geforce rtx 2080")}, cards)

	if got[0] != "0" {
		t.Errorf("assignInstances = %v, want the card matched onto instance 0", got)
	}
}

// Without Afterburner there is nothing to join against; the cards keep their own
// indices.
func TestWithoutAfterburnerCardsKeepTheirOwnIndex(t *testing.T) {
	got := assignInstances([]nvmlCard{card(0, "A"), card(1, "B")}, map[string]string{})

	if got[0] != "0" || got[1] != "1" {
		t.Errorf("assignInstances = %v, want [0 1]", got)
	}
}

// Whatever is assigned, no two cards may end up on one instance — that is the
// invariant the readings depend on.
func TestNoInstanceIsEverHandedOutTwice(t *testing.T) {
	for _, tc := range []struct {
		name  string
		cards map[string]string
		nvml  []nvmlCard
	}{
		{"all identical", map[string]string{"0": "X", "1": "X", "2": "X"},
			[]nvmlCard{card(0, "X"), card(1, "X"), card(2, "X")}},
		{"more nvml than afterburner", map[string]string{"0": "X"},
			[]nvmlCard{card(0, "X"), card(1, "X"), card(2, "Y")}},
		{"no names match", map[string]string{"0": "A", "1": "B"},
			[]nvmlCard{card(0, "C"), card(1, "D")}},
		{"nvml indices overlap afterburner", map[string]string{"0": "A"},
			[]nvmlCard{card(0, "Z"), card(0, "Z")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := assignInstances(tc.nvml, tc.cards)

			seen := map[string]bool{}
			for i, instance := range got {
				if instance == "" {
					t.Errorf("card %d got no instance", i)
				}
				if seen[instance] {
					t.Errorf("instance %q handed out twice: %v", instance, got)
				}
				seen[instance] = true
			}
		})
	}
}

// Readings must land under the instance that was assigned, not under a name
// that happens to collide.
func TestMergeWritesEachCardToItsOwnInstance(t *testing.T) {
	set := &metrics.Set{}
	cards := map[string]string{"0": "RTX 4090", "1": "RTX 4090"}
	nvml := []nvmlCard{
		{Index: 0, Name: "RTX 4090", VRAMTotalMB: 24576, VRAMUsedMB: 1024, hasVRAM: true},
		{Index: 1, Name: "RTX 4090", VRAMTotalMB: 24576, VRAMUsedMB: 8192, hasVRAM: true},
	}

	mergeFromNVML(set, nvml, cards)

	for instance, want := range map[string]float64{"0": 1024, "1": 8192} {
		reading, ok := set.Find(metrics.GPUVRAMUsed.ID, instance)
		if !ok {
			t.Errorf("no VRAM reading for card %s", instance)
			continue
		}
		if reading.Number != want {
			t.Errorf("card %s VRAM = %v, want %v", instance, reading.Number, want)
		}
	}
}

func TestDXGIFillsIntegratedGPUInventoryWithoutThirdPartyTools(t *testing.T) {
	set := &metrics.Set{}
	cards := map[string]string{}
	luids := map[string]windows.LUID{}
	adapters := []dxgiAdapter{{
		Index:                0,
		Name:                 "Intel(R) Iris(R) Xe Graphics",
		VendorID:             0x8086,
		DriverVersion:        "31.0.101.5590",
		LUID:                 windows.LUID{HighPart: 0, LowPart: 0x00017F59},
		DedicatedVideoMemory: 128 * mebibyte,
		SharedSystemMemory:   8 * 1024 * mebibyte,
	}}

	if !mergeFromDXGI(set, adapters, cards, luids) {
		t.Fatal("mergeFromDXGI reported no readings")
	}

	assertTextReading(t, set, metrics.GPUName, "0", "Intel(R) Iris(R) Xe Graphics")
	assertTextReading(t, set, metrics.GPUVendor, "0", "Intel")
	assertTextReading(t, set, metrics.GPUDriverVersion, "0", "31.0.101.5590")
	assertNumberReading(t, set, metrics.GPUDedicatedMemoryTotal, "0", 128)
	assertNumberReading(t, set, metrics.GPUSharedMemoryTotal, "0", 8192)

	// The identifier that lets the performance counters find this card again.
	if got := luids["0"]; got.LowPart != 0x00017F59 {
		t.Errorf("card 0 LUID = %+v, want the one DXGI reported", got)
	}
}

func TestDXGIPreservesExistingTelemetryAndFillsItsGaps(t *testing.T) {
	set := &metrics.Set{}
	set.Add(
		metrics.Text(metrics.GPUName, "0", "NVIDIA GeForce RTX 4090"),
		metrics.Gauge(metrics.GPUVRAMTotal, "0", 24576),
	)
	cards := map[string]string{"0": "NVIDIA GeForce RTX 4090"}

	mergeFromDXGI(set, []dxgiAdapter{{
		Index:                0,
		Name:                 "NVIDIA GeForce RTX 4090",
		VendorID:             0x10de,
		DedicatedVideoMemory: 16384 * mebibyte,
		SharedSystemMemory:   32768 * mebibyte,
	}}, cards, map[string]windows.LUID{})

	assertNumberReading(t, set, metrics.GPUVRAMTotal, "0", 24576)
	assertNumberReading(t, set, metrics.GPUDedicatedMemoryTotal, "0", 16384)
	assertNumberReading(t, set, metrics.GPUSharedMemoryTotal, "0", 32768)
	if got := countReadings(set, metrics.GPUName.ID, "0"); got != 1 {
		t.Errorf("GPU name appears %d times, want 1", got)
	}
}

func TestDXGIDedicatedMemoryAndNVMLVRAMStayDistinct(t *testing.T) {
	set := &metrics.Set{}
	cards := map[string]string{}
	mergeFromDXGI(set, []dxgiAdapter{{
		Index:                0,
		Name:                 "NVIDIA GeForce RTX 4090",
		VendorID:             0x10de,
		DedicatedVideoMemory: 24000 * mebibyte,
	}}, cards, map[string]windows.LUID{})

	mergeFromNVML(set, []nvmlCard{{
		Index: 0, Name: "NVIDIA GeForce RTX 4090",
		VRAMTotalMB: 24576, hasVRAM: true,
	}}, cards)

	assertNumberReading(t, set, metrics.GPUVRAMTotal, "0", 24576)
	assertNumberReading(t, set, metrics.GPUDedicatedMemoryTotal, "0", 24000)
	if got := countReadings(set, metrics.GPUVRAMTotal.ID, "0"); got != 1 {
		t.Errorf("GPU VRAM total appears %d times, want 1", got)
	}
	if got := countReadings(set, metrics.GPUDedicatedMemoryTotal.ID, "0"); got != 1 {
		t.Errorf("GPU dedicated memory appears %d times, want 1", got)
	}
}

func TestDXGIMatchesExistingCardsBeforeAssigningNewInstances(t *testing.T) {
	set := &metrics.Set{}
	cards := map[string]string{"0": "NVIDIA GeForce RTX 4070 Laptop GPU"}
	adapters := []dxgiAdapter{
		{Index: 0, Name: "Intel(R) Iris(R) Xe Graphics", VendorID: 0x8086},
		{Index: 1, Name: "NVIDIA GeForce RTX 4070 Laptop GPU", VendorID: 0x10de},
	}

	mergeFromDXGI(set, adapters, cards, map[string]windows.LUID{})

	if got := cards["0"]; got != "NVIDIA GeForce RTX 4070 Laptop GPU" {
		t.Errorf("card 0 = %q, want the existing NVIDIA card", got)
	}
	if got := cards["1"]; got != "Intel(R) Iris(R) Xe Graphics" {
		t.Errorf("card 1 = %q, want the newly discovered Intel card", got)
	}
}

func TestAfterburnerUsesDXGIInstancesInsteadOfItsOwnEnumerationOrder(t *testing.T) {
	set := &metrics.Set{}
	cards := map[string]string{
		"0": "Intel(R) Iris(R) Xe Graphics",
		"1": "NVIDIA GeForce RTX 4070 Laptop GPU",
	}
	snap := afterburner.Snapshot{
		GPUs: []afterburner.GPU{
			{Device: "NVIDIA GeForce RTX 4070 Laptop GPU"},
			{Device: "Intel(R) Iris(R) Xe Graphics"},
		},
		// The card a sensor belongs to is read out of its name, not out of a
		// field: "GPU1 usage" is Afterburner's first card.
		Entries: []afterburner.Entry{
			{Source: "GPU1 usage", Value: 75},
			{Source: "GPU2 usage", Value: 25},
		},
	}

	collectFromAfterburner(set, snap, cards)

	assertNumberReading(t, set, metrics.GPULoad, "0", 25)
	assertNumberReading(t, set, metrics.GPULoad, "1", 75)
}

func TestPCIAdapterVendorNames(t *testing.T) {
	for id, want := range map[uint32]string{
		0x1002: "AMD",
		0x10de: "NVIDIA",
		0x8086: "Intel",
	} {
		if got := vendorFromPCI(id); got != want {
			t.Errorf("vendorFromPCI(%#x) = %q, want %q", id, got, want)
		}
	}
	if got := vendorFromPCI(0xffff); got != "" {
		t.Errorf("vendorFromPCI(unknown) = %q, want empty", got)
	}
}

func TestPCIHardwareIDParsing(t *testing.T) {
	got, ok := parsePCIHardwareID(`PCI\VEN_10DE&DEV_1E87&SUBSYS_1E8710B0&REV_A1`)
	if !ok {
		t.Fatal("parsePCIHardwareID rejected a display adapter hardware ID")
	}
	want := pciAdapterID{VendorID: 0x10de, DeviceID: 0x1e87}
	if got != want {
		t.Errorf("parsePCIHardwareID = %+v, want %+v", got, want)
	}

	if _, ok := parsePCIHardwareID(`ROOT\VIRTUAL_DISPLAY`); ok {
		t.Error("parsePCIHardwareID accepted a non-PCI device")
	}
}

func TestPlugAndPlayInventoryRemovesSessionDuplicates(t *testing.T) {
	// Both mirrored entries carry the same subsystem id (SUBSYS_1E8710B0 on the
	// card this was measured on), which is why it is not the discriminator and
	// no longer carried. Vendor and device are what the match runs on.
	adapter := dxgiAdapter{
		Name: "NVIDIA GeForce RTX 2080", VendorID: 0x10de, DeviceID: 0x1e87,
	}
	adapters := []dxgiAdapter{adapter, adapter}
	devices := map[pciAdapterID]plugAndPlayAdapter{{
		VendorID: 0x10de, DeviceID: 0x1e87,
	}: {Count: 1, DriverVersion: "32.0.15.1234"}}

	got := limitAdaptersToPlugAndPlay(adapters, devices)
	if len(got) != 1 {
		t.Fatalf("kept %d adapters, want the one physical device", len(got))
	}
	if got[0].Index != 0 {
		t.Errorf("retained adapter index = %d, want compact index 0", got[0].Index)
	}
	if got[0].DriverVersion != "32.0.15.1234" {
		t.Errorf("retained adapter driver = %q, want Plug and Play version", got[0].DriverVersion)
	}
}

func TestPlugAndPlayInventoryPreservesTwoIdenticalPhysicalCards(t *testing.T) {
	adapter := dxgiAdapter{
		Name: "NVIDIA GeForce RTX 4090", VendorID: 0x10de, DeviceID: 0x2684,
	}
	adapters := []dxgiAdapter{adapter, adapter, adapter}
	devices := map[pciAdapterID]plugAndPlayAdapter{{
		VendorID: 0x10de, DeviceID: 0x2684,
	}: {Count: 2}}

	got := limitAdaptersToPlugAndPlay(adapters, devices)
	if len(got) != 2 {
		t.Fatalf("kept %d adapters, want both physical cards", len(got))
	}
	if got[0].Index != 0 || got[1].Index != 1 {
		t.Errorf("retained adapter indices = [%d %d], want [0 1]", got[0].Index, got[1].Index)
	}
}

func TestPlugAndPlayMatchingDoesNotDependOnOptionalSubsystemID(t *testing.T) {
	adapters := []dxgiAdapter{{
		Name: "Intel(R) Iris(R) Xe Graphics", VendorID: 0x8086, DeviceID: 0x46a8,
	}}
	devices := map[pciAdapterID]plugAndPlayAdapter{{
		VendorID: 0x8086, DeviceID: 0x46a8,
	}: {Count: 1}}

	got := limitAdaptersToPlugAndPlay(adapters, devices)
	if len(got) != 1 {
		t.Fatalf("kept %d adapters, want the Intel device despite the optional subsystem ID", len(got))
	}
}

func TestDXGIInventorySurvivesUnavailablePlugAndPlayInventory(t *testing.T) {
	adapters := []dxgiAdapter{{Index: 4, Name: "Intel Iris Xe", VendorID: 0x8086}}

	got := limitAdaptersToPlugAndPlay(adapters, nil)
	if len(got) != 1 || got[0].Name != "Intel Iris Xe" {
		t.Fatalf("fallback adapters = %+v, want the DXGI inventory", got)
	}
	if got[0].Index != 0 {
		t.Errorf("fallback adapter index = %d, want compact index 0", got[0].Index)
	}
}

func TestDXGIAdapterDescriptionMatchesWindowsABI(t *testing.T) {
	const wantSize = 312
	if got := unsafe.Sizeof(dxgiAdapterDesc1{}); got != wantSize {
		t.Fatalf("DXGI_ADAPTER_DESC1 size = %d, want %d bytes", got, wantSize)
	}
}

func assertTextReading(t *testing.T, set *metrics.Set, def metrics.Definition, instance, want string) {
	t.Helper()
	reading, ok := set.Find(def.ID, instance)
	if !ok {
		t.Fatalf("no %s reading for card %s", def.ID, instance)
	}
	if reading.Text != want {
		t.Errorf("%s card %s = %q, want %q", def.ID, instance, reading.Text, want)
	}
}

func assertNumberReading(t *testing.T, set *metrics.Set, def metrics.Definition, instance string, want float64) {
	t.Helper()
	reading, ok := set.Find(def.ID, instance)
	if !ok {
		t.Fatalf("no %s reading for card %s", def.ID, instance)
	}
	if reading.Number != want {
		t.Errorf("%s card %s = %v, want %v", def.ID, instance, reading.Number, want)
	}
}

func countReadings(set *metrics.Set, id, instance string) int {
	count := 0
	for _, reading := range set.Readings {
		if reading.Def.ID == id && reading.Instance == instance {
			count++
		}
	}
	return count
}
