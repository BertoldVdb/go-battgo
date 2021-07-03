package controller

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/BertoldVdb/go-battgo/phy"
	"github.com/BertoldVdb/go-misc/slotset"
)

// Controller is a module that runs as the host of the BattGO compatible network.
type Controller struct {
	phy    *phy.PHY
	newDev func(device *BusDevice) FunctionalDevice

	scanTimeMutex sync.Mutex
	scanTime      time.Time
	scanCount     int

	cmdSlotSet *slotset.SlotSet

	devices       map[string]*BusDevice
	devicesNumber int
	devicesMax    uint32

	addressUsed [4]uint64
}

type cmdData struct {
	addrResponse uint8
	response     []byte
}

// New creates a controller. You need to specify a PHY, the amount of devices on the bus and a callback
// that will be called when a new device is detected.
// If the number of devices is not known two special values can be given:
//   0: Scan continuously for new devices
//  -1: Scan periodically and whenever the number of visible devices is less than the maximum.
func New(phy *phy.PHY, numDevices int, newDev func(device *BusDevice) FunctionalDevice) *Controller {
	c := &Controller{
		phy:    phy,
		newDev: newDev,

		cmdSlotSet: slotset.New(1, func(slot *slotset.Slot) {
			slot.Data = &cmdData{}
		}),

		devicesNumber: numDevices,
		devices:       make(map[string]*BusDevice),
	}

	c.addressSetUsed(0x00, true) //Broadcast
	c.addressSetUsed(0x01, true) //Controller
	c.addressSetUsed(0xaa, true) //Escape

	c.phy.RXHandlePacket = c.rxHandlePacket
	if numDevices < 0 {
		c.phy.RXHandlePresense = func(b byte) error {
			c.detectStart()
			return nil
		}
	}

	return c
}

// GetMaxDevices returns the highest amount of devices ever seen.
func (c *Controller) GetMaxDevices() int {
	return int(atomic.LoadUint32(&c.devicesMax))
}

func (c *Controller) rxHandlePacket(addrSource uint8, addrDest uint8, payload []byte) error {
	if addrDest != 1 {
		return nil
	}

	return c.cmdSlotSet.IterateActive(func(slot *slotset.Slot) (bool, error) {
		data := slot.Data.(*cmdData)
		if data.addrResponse == addrSource {
			data.response = append(data.response[:0], payload...)
			slot.PostWithoutLock(nil)
		}
		return true, nil
	})
}

func (c *Controller) commandExec(ctx context.Context, addrDest uint8, addrResponse uint8, payload []byte, response []byte) ([]byte, error) {
	slot, err := c.cmdSlotSet.Get(ctx)
	if err != nil {
		return nil, err
	}
	defer c.cmdSlotSet.Put(slot)
	defer slot.Deactivate()

	data := slot.Data.(*cmdData)
	data.addrResponse = addrResponse
	data.response = response

	slot.Activate()
	err = c.phy.TXSendPacket(1, addrDest, payload)
	if err != nil {
		return nil, err
	}

	ok, err := slot.WaitCtx(ctx)
	if !ok || err != nil {
		return nil, err
	}

	slot.Deactivate()

	return data.response, nil
}

func (c *Controller) commandExecTimeout(timeout time.Duration, addrDest uint8, addrResponse uint8, payload []byte, response []byte) ([]byte, error) {
	if timeout == 0 {
		timeout = 150 * time.Millisecond
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	resp, err := c.commandExec(ctx, addrDest, addrResponse, payload, response)
	if ctx.Err() != nil {
		return nil, nil
	}

	return resp, err
}

func (c *Controller) addressFindFree() (byte, bool) {
	for i := 0; i < 254; i++ {
		mask := (uint64(1) << (i % 64))
		if (c.addressUsed[i/64] & mask) == 0 {
			c.addressSetUsed(byte(i), true)
			return byte(i), true
		}
	}
	return 0, false
}

func (c *Controller) addressSetUsed(addr byte, used bool) {
	mask := (uint64(1) << (addr % 64))
	if used {
		c.addressUsed[addr/64] |= mask
	} else {
		c.addressUsed[addr/64] &= ^mask
	}
}

// Run starts the controller and will return on an error or when Close() is called.
func (c *Controller) Run() error {
	go c.phy.Run()

	c.detectStart()

	for {
		err := c.detectAndConfigure()
		if err != nil {
			return err
		}

		for _, dev := range c.devices {
			if dev.isClosed() {
				err := dev.device.Disconnected()
				c.addressSetUsed(dev.address, false)
				delete(c.devices, string(dev.serial))
				if err != nil {
					return err
				}
				continue
			}

			active, err := dev.device.Access()
			if !active {
				dev.close()
			}

			if err != nil {
				return err
			}
		}
	}
}

// Make Run() return and close the underlying PHY.
func (c *Controller) Close() error {
	return c.phy.Close()
}
