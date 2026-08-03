//go:build windows

package winapi

import (
	"fmt"
	"strings"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// WindowsVersion is what a person would call this installation, e.g.
// "Windows 11 Pro 24H2 (26100.2314)".
//
// Assembled rather than read, because no single source has it. The edition name
// lives in the registry, the build comes from RtlGetVersion — which reports the
// truth, while GetVersionEx lies to unmanifested programs and would call every
// Windows since 8.1 "6.2".
//
// The awkward part is that the registry's ProductName still reads "Windows 10
// Pro" on Windows 11: Microsoft never updated it, and a program that trusts it
// tells half its users the wrong operating system. The build number is the only
// honest discriminator — 22000 and above is Windows 11.
func WindowsVersion() string {
	version := windows.RtlGetVersion()
	if version == nil {
		return ""
	}

	product, display, revision := versionFromRegistry()

	// Correct the product name against the build rather than believing it.
	name := product
	switch {
	case version.BuildNumber >= 22000:
		name = strings.Replace(product, "Windows 10", "Windows 11", 1)
	case name == "":
		name = fmt.Sprintf("Windows %d.%d", version.MajorVersion, version.MinorVersion)
	}

	build := fmt.Sprintf("%d", version.BuildNumber)
	if revision != "" {
		build += "." + revision
	}
	if display != "" {
		return fmt.Sprintf("%s %s (%s)", name, display, build)
	}
	return fmt.Sprintf("%s (%s)", name, build)
}

// versionFromRegistry reads the parts RtlGetVersion does not carry. Every one
// of them is optional: a missing value means a shorter description, never a
// wrong one.
func versionFromRegistry() (product, display, revision string) {
	key, err := registry.OpenKey(registry.LOCAL_MACHINE,
		`SOFTWARE\Microsoft\Windows NT\CurrentVersion`, registry.QUERY_VALUE)
	if err != nil {
		return "", "", ""
	}
	defer key.Close()

	product, _, _ = key.GetStringValue("ProductName")
	display, _, _ = key.GetStringValue("DisplayVersion")
	if display == "" {
		// Before 21H2 the same fact was called ReleaseId.
		display, _, _ = key.GetStringValue("ReleaseId")
	}
	if ubr, _, err := key.GetIntegerValue("UBR"); err == nil {
		revision = fmt.Sprintf("%d", ubr)
	}
	return strings.TrimSpace(product), strings.TrimSpace(display), revision
}

// maxProcesses bounds the buffer. A Windows session runs a few hundred; this
// only stops a runaway loop on a machine doing something extraordinary.
const maxProcesses = 1 << 16

// ProcessCount is how many processes are running.
//
// EnumProcesses does not report that the buffer was too small — it fills what
// fits and says so only by returning exactly as many bytes as were offered. The
// buffer therefore grows until the answer is smaller than the room given for it,
// which is the only way to know the list was complete.
func ProcessCount() (int, error) {
	for size := 1024; size <= maxProcesses; size *= 2 {
		ids := make([]uint32, size)
		var returned uint32

		if err := windows.EnumProcesses(ids, &returned); err != nil {
			return 0, fmt.Errorf("EnumProcesses: %w", err)
		}

		const idSize = 4
		count := int(returned) / idSize
		if count < size {
			return count, nil
		}
	}
	return 0, fmt.Errorf("more than %d processes", maxProcesses)
}
