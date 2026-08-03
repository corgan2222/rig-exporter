//go:build windows

package pawnio

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

// The library's own header, PawnIOLib.h, is installed alongside the DLL and is
// the authority for these signatures. Every entry point returns an HRESULT and
// takes nothing more complicated than a pointer and a length — there is not one
// struct crossing the boundary, which is why this binding needs no layout
// assertions and cannot be got subtly wrong.
//
//	HRESULT pawnio_version(PULONG version);      // (major<<16)|(minor<<8)|patch
//	HRESULT pawnio_open(PHANDLE handle);
//	HRESULT pawnio_load(HANDLE, const UCHAR* blob, SIZE_T size);
//	HRESULT pawnio_execute(HANDLE, PCSTR name, const ULONG64* in, SIZE_T in_size,
//	                       PULONG64 out, SIZE_T out_size, PSIZE_T return_size);
//	HRESULT pawnio_close(HANDLE handle);

const (
	sOK             = 0
	eAccessDenied   = 0x80070005
	libraryFileName = "PawnIOLib.dll"
)

type library struct {
	once sync.Once
	dll  *windows.LazyDLL
	ok   bool

	version proc
	open    proc
	load    proc
	execute proc
	close   proc
}

// proc is one entry point plus whether this installation has it.
//
// windows.LazyProc.Call resolves through mustFind, which panics rather than
// returning an error, and this program is linked with -H windowsgui where a
// panic disappears without a window. PawnIO's API may well grow, so nothing is
// called before it is known to be there.
type proc struct {
	p  *windows.LazyProc
	ok bool
}

func (c proc) call(args ...uintptr) (uintptr, bool) {
	if !c.ok {
		return 0, false
	}
	hr, _, _ := c.p.Call(args...)
	return hr, true
}

var lib library

// loadLibrary finds PawnIOLib.dll once per process.
func (l *library) loadLibrary() bool {
	l.once.Do(func() {
		for _, candidate := range libraryPaths() {
			dll := windows.NewLazyDLL(candidate)
			if dll.Load() != nil {
				continue
			}
			l.dll = dll
			break
		}
		if l.dll == nil {
			return
		}

		l.version = l.resolve("pawnio_version")
		l.open = l.resolve("pawnio_open")
		l.load = l.resolve("pawnio_load")
		l.execute = l.resolve("pawnio_execute")
		l.close = l.resolve("pawnio_close")

		// Without these four there is nothing to do, and pretending otherwise
		// would only move the failure somewhere less obvious.
		l.ok = l.version.ok && l.open.ok && l.load.ok && l.execute.ok
	})
	return l.ok
}

func (l *library) resolve(name string) proc {
	p := l.dll.NewProc(name)
	return proc{p: p, ok: p.Find() == nil}
}

// libraryPaths lists where the library might be, best guess first.
//
// The bare name goes first so a machine that has it on the search path is
// served without touching the disk. The installed location is derived from the
// environment rather than hardcoded, because "C:\Program Files" is not where
// every Windows keeps its programs — a localised or relocated installation
// would otherwise look like an absent one.
func libraryPaths() []string {
	paths := []string{libraryFileName}
	for _, key := range []string{"ProgramFiles", "ProgramW6432", "ProgramFiles(x86)"} {
		if base := os.Getenv(key); base != "" {
			paths = append(paths, filepath.Join(base, "PawnIO", libraryFileName))
		}
	}
	return paths
}

// Detect reports what can be done with PawnIO here.
//
// The two questions are asked separately on purpose. The library loads and
// reports its version without any privilege at all, so "is it installed" can be
// answered honestly from an ordinary process; only opening the device runs into
// the ACL. That is what lets the interface say "installed, but this program
// needs elevation" rather than the useless "unavailable".
func Detect() State {
	if !lib.loadLibrary() {
		return State{Availability: NotInstalled, Detail: "PawnIOLib.dll could not be loaded"}
	}

	state := State{Version: version()}

	handle, hr, err := openDevice()
	switch {
	case err == nil:
		closeDevice(handle)
		state.Availability = Ready
	case hr == eAccessDenied:
		state.Availability = NeedsElevation
		state.Detail = "pawnio_open returned E_ACCESSDENIED"
	default:
		state.Availability = DriverUnavailable
		state.Detail = err.Error()
	}
	return state
}

func version() string {
	var raw uint32
	hr, ok := lib.version.call(uintptr(unsafe.Pointer(&raw)))
	if !ok || hr != sOK {
		return ""
	}
	return fmt.Sprintf("%d.%d.%d", raw>>16, (raw>>8)&0xFF, raw&0xFF)
}

// openDevice opens an executor. The raw HRESULT comes back alongside the error
// because the caller has to tell "refused" from "broken".
func openDevice() (windows.Handle, uint32, error) {
	var handle windows.Handle
	hr, ok := lib.open.call(uintptr(unsafe.Pointer(&handle)))
	if !ok {
		return 0, 0, fmt.Errorf("pawnio_open is missing from PawnIOLib")
	}
	if uint32(hr) != sOK {
		return 0, uint32(hr), fmt.Errorf("pawnio_open returned 0x%08X", uint32(hr))
	}
	return handle, sOK, nil
}

func closeDevice(handle windows.Handle) {
	lib.close.call(uintptr(handle))
}
