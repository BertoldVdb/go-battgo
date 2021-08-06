package battery

import (
	"bytes"
	"encoding/binary"
	"sync"
	"time"

	"encoding/hex"

	"github.com/BertoldVdb/go-battgo/controller"
)

type BatteryType int

const (
	BatteryTypeLiHv  BatteryType = 0
	BatteryTypeLiPo  BatteryType = 1
	BatteryTypeLiIon BatteryType = 2
	BatteryTypeLiFe  BatteryType = 3
	BatteryTypePb    BatteryType = 5
	BatteryTypeNiMH  BatteryType = 6
)

type BatteryData struct {
	sync.RWMutex

	Connected bool
	LastData  time.Time

	BusAddress       uint8
	Serial           string
	ManufacturerName string

	BatteryType                 BatteryType
	CellDischargeCutOffV        float32
	CellDischargeNormalV        float32
	CellChargeMaxV              float32
	CellStorageDefaultV         float32
	CellCapacityAh              float32
	BatteryChargeMaxCurrentA    float32
	BatteryDischargeMaxCurrentA float32
	TempUseLowC                 int
	TempUseHighC                int
	TempStorageLowC             int
	TempStorageHighC            int
	BatteryHasAutoDischarge     bool
	BatteryNumberOfCells        int

	BatteryPreferredChargeCurrentA float32
	CellPreferredStorageVoltageV   float32
	CellPreferredMaxVoltageV       float32
	BatterySelfDischargeEnabled    bool
	BatterySelfDischargeHours      int

	BatteryChargeCycles         int
	BatteryErrorOverCharged     int
	BatteryErrorOverDischarged  int
	BatteryErrorOverTemperature int

	TempCurrentC int
	CellVoltageV []float32
}

type DeviceBattery struct {
	parent *controller.BusDevice

	currentState []byte
	cycleInfo    []byte
	serial       []byte
	factoryInfo  []byte
	userSettings []byte

	Data       BatteryData
	updateChan chan<- (*DeviceBattery)

	readIndex int
}

// New creates a device representing a standard BattGO compatible battery. When the internal data is updated,
// it will write a reference to itself on updateChan.
func New(device *controller.BusDevice, updateChan chan<- (*DeviceBattery)) controller.FunctionalDevice {
	d := &DeviceBattery{
		parent:     device,
		updateChan: updateChan,
	}

	d.Data.Serial = hex.EncodeToString(device.GetSerial())
	d.Data.BusAddress = device.GetAddress()
	d.Data.Connected = true

	return d
}

func (d *DeviceBattery) deltaSerial() (bool, error) {
	if len(d.serial) < 11 || !bytes.Equal(d.serial[1:11], d.parent.GetSerial()) {
		return false, nil
	}

	index := bytes.IndexByte(d.serial[11:], 0)
	if index < 0 {
		index = len(d.serial)
	}

	d.Data.Lock()
	defer d.Data.Unlock()
	name := d.serial[11:(11 + index)]
	if !bytes.Equal([]byte(d.Data.ManufacturerName), name) {
		d.Data.ManufacturerName = string(name)
	}

	return true, nil
}

func (d *DeviceBattery) deltaFactoryData() (bool, error) {
	if len(d.factoryInfo) < 24 {
		return false, nil
	}

	d.Data.Lock()
	defer d.Data.Unlock()
	d.Data.BatteryType = BatteryType(d.factoryInfo[1])
	d.Data.CellDischargeCutOffV = float32(binary.LittleEndian.Uint16(d.factoryInfo[2:4])) / 1000.0
	d.Data.CellDischargeNormalV = float32(binary.LittleEndian.Uint16(d.factoryInfo[4:6])) / 1000.0
	d.Data.CellChargeMaxV = float32(binary.LittleEndian.Uint16(d.factoryInfo[6:8])) / 1000.0
	d.Data.CellStorageDefaultV = float32(binary.LittleEndian.Uint16(d.factoryInfo[8:10])) / 1000.0
	d.Data.CellCapacityAh = float32(binary.LittleEndian.Uint32(d.factoryInfo[10:14])) / 1000.0
	d.Data.BatteryChargeMaxCurrentA = float32(binary.LittleEndian.Uint16(d.factoryInfo[14:16])) / 10.0 * d.Data.CellCapacityAh
	d.Data.BatteryDischargeMaxCurrentA = float32(binary.LittleEndian.Uint16(d.factoryInfo[16:18])) / 10.0 * d.Data.CellCapacityAh
	d.Data.TempUseLowC = int(int8(d.factoryInfo[18]))
	d.Data.TempUseHighC = int(int8(d.factoryInfo[19]))
	d.Data.TempStorageLowC = int(int8(d.factoryInfo[20]))
	d.Data.TempStorageHighC = int(int8(d.factoryInfo[21]))
	d.Data.BatteryHasAutoDischarge = d.factoryInfo[22] > 0
	d.Data.BatteryNumberOfCells = int(d.factoryInfo[23])

	return true, nil
}

func (d *DeviceBattery) deltaUser() (bool, error) {
	if len(d.userSettings) < 9 {
		return false, nil
	}

	d.Data.Lock()
	defer d.Data.Unlock()
	d.Data.BatteryPreferredChargeCurrentA = float32(binary.LittleEndian.Uint32(d.userSettings[1:5])&0xFFFFFF) / 1000.0
	d.Data.CellPreferredStorageVoltageV = float32(binary.LittleEndian.Uint16(d.userSettings[4:6])) / 1000.0
	d.Data.CellPreferredMaxVoltageV = float32(binary.LittleEndian.Uint16(d.userSettings[6:8])) / 1000.0
	d.Data.BatterySelfDischargeEnabled = d.userSettings[8] != 0xFF
	d.Data.BatterySelfDischargeHours = int(d.userSettings[8])

	return true, nil
}

func (d *DeviceBattery) deltaCycle() (bool, error) {
	if len(d.cycleInfo) < 12 {
		return false, nil
	}

	d.Data.Lock()
	defer d.Data.Unlock()
	d.Data.BatteryChargeCycles = int(binary.LittleEndian.Uint16(d.cycleInfo[1:3]))
	d.Data.BatteryErrorOverTemperature = int(binary.LittleEndian.Uint16(d.cycleInfo[6:8]))
	d.Data.BatteryErrorOverCharged = int(binary.LittleEndian.Uint16(d.cycleInfo[8:10]))
	d.Data.BatteryErrorOverDischarged = int(binary.LittleEndian.Uint16(d.cycleInfo[10:12]))

	return true, nil
}

func (d *DeviceBattery) deltaState() (bool, error) {
	if len(d.currentState) < 6 || d.currentState[1] != 0 {
		return false, nil
	}

	d.Data.Lock()
	defer d.Data.Unlock()

	numCell := int(d.currentState[2]) + 1
	if len(d.Data.CellVoltageV) != numCell {
		d.Data.CellVoltageV = make([]float32, numCell)
	}

	if len(d.currentState) < 3+1+2*numCell {
		return false, nil
	}

	index := 3
	for i := 0; i < numCell; i++ {
		d.Data.CellVoltageV[i] = float32(binary.LittleEndian.Uint16(d.currentState[index:])) / 1000.0
		index += 2
	}

	d.Data.TempCurrentC = int(int8(d.currentState[index]))

	d.Data.LastData = time.Now()

	return true, nil
}

func (d *DeviceBattery) readData(cmd []byte, expectedReply uint8, destination *[]byte, deltaFunc func() (bool, error)) (bool, error) {
	var rxBuf [256]byte

	response, err := d.parent.CommandExecTimeout(0, cmd, rxBuf[:])
	if response == nil {
		return false, err
	}

	if len(response) == 0 || response[0] != expectedReply {
		return false, err
	}

	if !bytes.Equal(response, *destination) {
		cpy := *destination
		if len(cpy) != len(response) {
			cpy = make([]byte, len(response))
		}
		copy(cpy, response)
		*destination = cpy

		if deltaFunc != nil {
			return deltaFunc()
		}
	}

	return true, err
}

func (d *DeviceBattery) signalUpdate() {
	select {
	case d.updateChan <- d:
	default:
	}
}

// Access is an internal function that should only be called by the controller.
func (d *DeviceBattery) Access() (bool, error) {
	d.readIndex++
	switch d.readIndex {
	case 0:
		numCell := d.Data.BatteryNumberOfCells
		if numCell == 0 {
			numCell = 8
		}
		ok, err := d.readData([]byte{0x44, 0, byte(d.Data.BatteryNumberOfCells - 1)}, 0x45, &d.currentState, d.deltaState)

		d.signalUpdate()

		return ok, err
	case 1:
		return d.readData([]byte{0x4A}, 0x4B, &d.cycleInfo, d.deltaCycle)
	case 2:
		return d.readData([]byte{0x42}, 0x43, &d.userSettings, d.deltaUser)
	case 3:
		return d.readData([]byte{0x84}, 0x85, &d.serial, d.deltaSerial)
	default:
		d.readIndex = -1
		return d.readData([]byte{0x88}, 0x89, &d.factoryInfo, d.deltaFactoryData)
	}
}

// Disconnected is an internal function that should only be called by the controller.
func (d *DeviceBattery) Disconnected() error {
	d.Data.Lock()
	d.Data.Connected = false
	d.Data.Unlock()

	d.signalUpdate()
	return nil
}

// SetConfiguration will write a new configuration to the battery.
// Self discharge is disabled when dischargeHours is negative.
func (d *DeviceBattery) SetConfiguration(chargeCurrentA float32, storageVoltageV float32, maxVoltageV float32, dischargeHours float32) (bool, error) {
	var buf [9]byte
	buf[0] = 0x46
	binary.LittleEndian.PutUint32(buf[1:], uint32(chargeCurrentA*1000))
	binary.LittleEndian.PutUint16(buf[4:], uint16(storageVoltageV*1000))
	binary.LittleEndian.PutUint16(buf[6:], uint16(maxVoltageV*1000))
	if dischargeHours < 0 {
		buf[8] = 0xFF
	} else {
		buf[8] = uint8(dischargeHours)
	}

	response, err := d.parent.CommandExecTimeout(time.Second, buf[:], nil)
	if err != nil {
		return false, err
	}

	return len(response) == 2 && response[0] == 0x47, nil
}
