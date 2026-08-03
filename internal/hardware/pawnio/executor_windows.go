//go:build windows

package pawnio

import (
	"fmt"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Executor is an open PawnIO device with one module loaded.
//
// The device is opened once and kept: opening it is the expensive part, and the
// collection loop asks for a temperature several times a second.
type Executor struct {
	mu     sync.Mutex
	handle windows.Handle
	open   bool
}

// NewExecutor opens the device and loads a module into it.
//
// The module's signature is checked by the driver, not here. That is the whole
// point of PawnIO: this program hands over a blob it did not write and could
// not verify, and the driver refuses to run anything that namazso did not sign.
func NewExecutor(module []byte) (*Executor, error) {
	if !lib.loadLibrary() {
		return nil, fmt.Errorf("PawnIOLib is not available")
	}
	if len(module) == 0 {
		return nil, fmt.Errorf("the module is empty")
	}

	handle, _, err := openDevice()
	if err != nil {
		return nil, err
	}

	hr, ok := lib.load.call(
		uintptr(handle),
		uintptr(unsafe.Pointer(&module[0])),
		uintptr(len(module)),
	)
	if !ok {
		closeDevice(handle)
		return nil, fmt.Errorf("pawnio_load is missing from PawnIOLib")
	}
	if uint32(hr) != sOK {
		closeDevice(handle)
		return nil, fmt.Errorf("pawnio_load returned 0x%08X — the module was refused", uint32(hr))
	}
	return &Executor{handle: handle, open: true}, nil
}

// Execute runs a function in the loaded module.
//
// in and out are counts of 64-bit words, which is the only shape this interface
// has. out is sized by the caller and the number actually written comes back,
// because a function may return fewer values than there was room for.
func (e *Executor) Execute(name string, in []uint64, outWords int) ([]uint64, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if !e.open {
		return nil, fmt.Errorf("the executor is closed")
	}

	fn, err := windows.BytePtrFromString(name)
	if err != nil {
		return nil, err
	}

	// Taking the address of the first element needs there to be one. A zero
	// length with a nil pointer is what the library expects for "no input".
	var inPtr *uint64
	if len(in) > 0 {
		inPtr = &in[0]
	}
	out := make([]uint64, outWords)
	var outPtr *uint64
	if outWords > 0 {
		outPtr = &out[0]
	}

	var written uintptr
	hr, ok := lib.execute.call(
		uintptr(e.handle),
		uintptr(unsafe.Pointer(fn)),
		uintptr(unsafe.Pointer(inPtr)),
		uintptr(len(in)),
		uintptr(unsafe.Pointer(outPtr)),
		uintptr(outWords),
		uintptr(unsafe.Pointer(&written)),
	)
	if !ok {
		return nil, fmt.Errorf("pawnio_execute is missing from PawnIOLib")
	}
	if uint32(hr) != sOK {
		return nil, fmt.Errorf("%s returned 0x%08X", name, uint32(hr))
	}
	if int(written) > outWords {
		// Cannot happen from a sane driver, and if it ever did the buffer
		// behind out would already be overrun. Refusing the reading is all
		// that is left, and it is better than handing on a length that says
		// more was written than there was room for.
		return nil, fmt.Errorf("%s reported %d values into room for %d", name, written, outWords)
	}
	return out[:written], nil
}

// Close releases the device.
func (e *Executor) Close() {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.open {
		closeDevice(e.handle)
		e.open = false
	}
}

// ReadSMN reads a System Management Network register, which is where AMD keeps
// the thermal block.
func (e *Executor) ReadSMN(offset uint32) (uint64, error) {
	out, err := e.Execute("ioctl_read_smn", []uint64{uint64(offset)}, 1)
	if err != nil {
		return 0, err
	}
	if len(out) == 0 {
		return 0, fmt.Errorf("ioctl_read_smn returned nothing")
	}
	return out[0], nil
}

// ReadMSR reads a model-specific register.
func (e *Executor) ReadMSR(index uint32) (uint64, error) {
	out, err := e.Execute("ioctl_read_msr", []uint64{uint64(index)}, 1)
	if err != nil {
		return 0, err
	}
	if len(out) == 0 {
		return 0, fmt.Errorf("ioctl_read_msr returned nothing")
	}
	return out[0], nil
}
