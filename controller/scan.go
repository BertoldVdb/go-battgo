package controller

import (
	"sync/atomic"
	"time"
)

func (c *Controller) detectStart() {
	c.scanTimeMutex.Lock()
	defer c.scanTimeMutex.Unlock()
	c.scanTime = time.Now().Add(20 * time.Second)
}

func (c *Controller) detectAndConfigure() error {
	devicesMax := int(atomic.LoadUint32(&c.devicesMax))
	if len(c.devices) > devicesMax {
		atomic.StoreUint32(&c.devicesMax, uint32(len(c.devices)))
	}

	if c.devicesNumber >= 0 {
		if c.devicesNumber > 0 && len(c.devices) >= c.devicesNumber {
			return nil
		}
	} else {
		c.scanTimeMutex.Lock()
		scanTime := c.scanTime
		c.scanTimeMutex.Unlock()

		if len(c.devices) >= devicesMax && devicesMax > 0 && time.Now().After(scanTime) {
			c.scanCount++
			if c.scanCount >= 10 {
				c.scanCount = 0
			} else {
				return nil
			}
		}
	}

	if len(c.devices) == 0 && c.phy.TXSendBreak != nil {
		c.phy.TXSendBreak(200 * time.Millisecond)
		time.Sleep(30 * time.Millisecond)
	}

	cmdPingAll := [12]byte{2}

	response, err := c.commandExecTimeout(0, 0, 0, cmdPingAll[:], nil)
	if err != nil {
		return err
	}

	if len(response) == 11 && response[0] == 3 {
		dev, ok := c.devices[string(response[1:])]
		if !ok {
			address, ok := c.addressFindFree()
			if !ok {
				return nil
			}

			dev = &BusDevice{
				controller: c,
				serial:     response[1:],
				address:    address,
			}

			dev.device = c.newDev(dev)
			if dev.device == nil {
				dev.device = &dummyDevice{}
			}
			c.devices[string(response[1:])] = dev
		}

		cmdSetAddress := [12]byte{2}
		cmdSetAddress[1] = dev.address
		copy(cmdSetAddress[2:], response[1:])

		response, err = c.commandExecTimeout(0, 0, dev.address, cmdSetAddress[:], nil)
		if err != nil {
			return err
		}

		if len(response) != 11 || response[0] != 3 {
			dev.close()
		}
	}

	return nil
}
