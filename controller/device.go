package controller

import (
	"context"
	"errors"
	"sync"
	"time"
)

var (
	// ErrorClosed is returned when the device has already been closed.
	ErrorClosed = errors.New("Device has been closed")
)

// BusDevice represents a device on the BattGO compatible bus.
type BusDevice struct {
	sync.Mutex

	controller *Controller
	serial     []byte
	address    uint8
	closed     bool

	device FunctionalDevice
}

func (d *BusDevice) close() {
	d.Lock()
	defer d.Unlock()

	d.closed = true
}

// GetSerial returns the serial of the device.
func (d *BusDevice) GetSerial() []byte {
	return d.serial
}

// GetAddres returns the physical address of the device.
func (d *BusDevice) GetAddress() uint8 {
	return d.address
}

func (d *BusDevice) isClosed() bool {
	d.Lock()
	defer d.Unlock()

	return d.closed
}

// CommandExec sends a message to the device and returns the response. You can provide a slice
// that will be used to store the response.
func (d *BusDevice) CommandExec(ctx context.Context, payload []byte, response []byte) ([]byte, error) {
	if d.isClosed() {
		return nil, ErrorClosed
	}

	return d.controller.commandExec(ctx, d.address, d.address, payload, response)
}

// CommandExecTimeout sends a message to the device and returns the response. You can provide a slice
// that will be used to store the response. Convenience logic is provided to handle timeouts. When the
// timeout value is 0, a reasonable default is used.
func (d *BusDevice) CommandExecTimeout(timeout time.Duration, payload []byte, response []byte) ([]byte, error) {
	if d.isClosed() {
		return nil, ErrorClosed
	}

	return d.controller.commandExecTimeout(timeout, d.address, d.address, payload, response)
}

// FunctionalDevice represents code implementing the interface to a BattGO compatible device.
type FunctionalDevice interface {
	// Access is called periodically by the controller. The module should perform periodic actions.
	// If communications to the device failed, the boolean value should be false.
	// If error is not nil, the controller Run() function will terminate with this error.
	Access() (bool, error)

	// Disconnected will be called by the controller when it appears the device left the bus.
	Disconnected() error
}

type dummyDevice struct {
}

func (d *dummyDevice) Access() (bool, error) {
	return true, nil
}
func (d *dummyDevice) Disconnected() error {
	return nil
}
