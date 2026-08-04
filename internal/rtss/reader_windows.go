//go:build windows

package rtss

import (
	"errors"
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/corgan2222/rig-exporter/internal/winapi"
)

// Reader reads the RTSS shared memory. It holds no state: the mapping is
// opened per read so that starting or restarting RTSS is picked up without
// any reconnect logic here.
type Reader struct{}

// Available reports whether the shared memory can be opened and looks like
// RTSS. It is what the startup check and the RTSS diagnostic sensor use.
func (r Reader) Available() error {
	_, err := r.Read()
	return err
}

// Read maps the shared memory, copies out the part Parse needs and decodes it.
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

	// The header tells us how much of the mapping is actually in use, which
	// keeps the copy to a few hundred kilobytes instead of the whole view.
	header := unsafe.Slice((*byte)(unsafe.Pointer(addr)), headerSize)
	size, err := MappingSize(header)
	if err != nil {
		return Snapshot{}, err
	}

	// Copy before parsing: RTSS keeps writing to this memory, and Parse must
	// not hand out strings that alias a mapping we are about to unmap.
	buf := make([]byte, size)
	copy(buf, unsafe.Slice((*byte)(unsafe.Pointer(addr)), size))

	return Parse(buf)
}
