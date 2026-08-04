//go:build windows

package afterburner

import (
	"errors"
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/corgan2222/rig-exporter/internal/winapi"
)

// Reader reads the Afterburner shared memory. It holds no state: the mapping
// is opened per read, so starting or restarting Afterburner is picked up
// without any reconnect logic here.
type Reader struct{}

// Available reports whether the shared memory can be read right now.
func (r Reader) Available() error {
	_, err := r.Read()
	return err
}

// Read maps the shared memory, copies out what Parse needs and decodes it.
func (Reader) Read() (Snapshot, error) {
	handle, err := winapi.OpenFileMapping(windows.FILE_MAP_READ, false, MappingName)
	if err != nil {
		switch {
		case errors.Is(err, windows.ERROR_FILE_NOT_FOUND), errors.Is(err, windows.ERROR_PATH_NOT_FOUND):
			return Snapshot{}, ErrNotRunning
		case errors.Is(err, windows.ERROR_ACCESS_DENIED):
			return Snapshot{}, ErrAccessDenied
		default:
			return Snapshot{}, fmt.Errorf("open %s: %w", MappingName, err)
		}
	}
	defer windows.CloseHandle(handle)

	addr, err := windows.MapViewOfFile(handle, windows.FILE_MAP_READ, 0, 0, 0)
	if err != nil {
		if errors.Is(err, windows.ERROR_ACCESS_DENIED) {
			return Snapshot{}, ErrAccessDenied
		}
		return Snapshot{}, fmt.Errorf("map %s: %w", MappingName, err)
	}
	defer windows.UnmapViewOfFile(addr)

	header := unsafe.Slice((*byte)(unsafe.Pointer(addr)), minHeaderSize)
	size, err := MappingSize(header)
	if err != nil {
		return Snapshot{}, err
	}

	// The size derived from the header reserves room for the GPU array, which
	// can overshoot the real mapping. Clamp to what is actually committed.
	if committed := committedBytes(addr); committed > 0 && size > committed {
		size = committed
	}

	// Copy before parsing: Afterburner keeps writing to this memory, and Parse
	// must not hand out strings that alias a mapping we are about to unmap.
	buf := make([]byte, size)
	copy(buf, unsafe.Slice((*byte)(unsafe.Pointer(addr)), size))

	return Parse(buf)
}

// committedBytes reports how large the mapped region really is, or 0 when it
// cannot be determined.
func committedBytes(addr uintptr) int {
	var info windows.MemoryBasicInformation
	if err := windows.VirtualQuery(addr, &info, unsafe.Sizeof(info)); err != nil {
		return 0
	}
	return int(info.RegionSize)
}
