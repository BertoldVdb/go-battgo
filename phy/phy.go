package phy

import (
	"encoding/binary"
	"io"
	"time"

	"github.com/BertoldVdb/go-misc/serial"
)

// PHY implements functions for receiving and transmitting data to ISDT BattGO devices.
type PHY struct {
	// Port is the device used to communicate with the target.
	Port io.ReadWriteCloser

	// RXHandlePresense is an optional callback that is called when a target device may be present.
	RXHandlePresense func(byte) error

	// RXHandlePacket is a callback that is called each time a valid packet is received. Please note
	// that, depending on your hardware configuration, you may receive your own packets.
	RXHandlePacket func(addrSource uint8, addrDest uint8, payload []byte) error

	// TXDisableScrambler disables scrambling on outgoing packets when set.
	TXDisableScrambler bool

	// TXSendBreak is a callback that should send a low state on the serial line for at least
	// the given duration. If your hardware does not allow duration control, ensure it is at
	// least 70ms.
	TXSendBreak func(t time.Duration) error

	txBuf  []byte
	txSeed uint8
}

func scramble(seed uint8, out []byte, in []byte) {
	xor := seed + 136

	for i := range in {
		out[i] = in[i] ^ xor

		xor += seed
		xor ^= seed
	}
}

// Run needs to be called to start listening for packets on the line. It will return
// when there is an error or Close() is called.
func (b *PHY) Run() error {
	defer b.Close()

	rxState := 0
	rxLen := 0

	var addrSource, addrDest uint8
	var sum uint16
	var payload []byte
	var rxBuf [512]byte
	var isEscaped bool

	for {
		n, err := b.Port.Read(rxBuf[:])
		if err != nil {
			return err
		}
		message := rxBuf[:n]

		for _, m := range message {
			if !isEscaped {
				if m == 0xAA {
					isEscaped = true
					continue
				}
			} else {
				isEscaped = false
				if m != 0xAA {
					rxState = 1
					sum = 0
				}
			}

			switch rxState {
			case 0:
				if b.RXHandlePresense != nil {
					err := b.RXHandlePresense(m)
					if err != nil {
						return err
					}
				}
			case 1:
				addrSource = m
				sum += uint16(m)
				rxState = 2
			case 2:
				addrDest = m
				sum += uint16(m)
				rxState = 3
			case 3:
				if m > 0 {
					payload = payload[:0]
					rxLen = int(m) + 2
					sum += uint16(m)
					rxState = 4
				} else {
					rxState = 0
				}
			case 4:
				payload = append(payload, m)
				if len(payload) == rxLen {
					for i := 0; i < len(payload)-2; i++ {
						sum += uint16(payload[i])
					}

					/* Checksum valid? */
					csumEnd := len(payload) - 2
					if binary.LittleEndian.Uint16(payload[csumEnd:]) == sum {
						scramble(payload[0], payload[1:], payload[1:])

						if b.RXHandlePacket != nil {
							err := b.RXHandlePacket(addrSource, addrDest, payload[1:csumEnd])
							if err != nil {
								return err
							}
						}
					}

					rxState = 0
				}
			default:
				rxState = 0
			}
		}
	}
}

// TXSendPacket encode and sends a packet to the remote device.
func (b *PHY) TXSendPacket(addrSource uint8, addrDest uint8, payload []byte) error {
	var sum uint16

	addByte := func(m byte) {
		sum += uint16(m)

		if m == 0xAA {
			b.txBuf = append(b.txBuf, 0xAA)
		}
		b.txBuf = append(b.txBuf, m)
	}

	b.txBuf = append(b.txBuf[:0], 0xAA)
	addByte(addrSource)
	addByte(addrDest)
	addByte(byte(len(payload) + 1))

	var payloadScrambled []byte
	if b.TXDisableScrambler {
		addByte(120)
		payloadScrambled = payload
	} else {
		addByte(b.txSeed)
		payloadScrambled = make([]byte, len(payload))
		scramble(b.txSeed, payloadScrambled, payload)
		b.txSeed++
	}

	for _, m := range payloadScrambled {
		addByte(m)
	}

	finalSum := sum
	addByte(byte(finalSum))
	addByte(byte(finalSum >> 8))

	_, err := b.Port.Write(b.txBuf)
	return err
}

// Close stops Run() and also closes the underlying io.Closer
func (b *PHY) Close() error {
	return b.Port.Close()
}

// NewSerialSimple is a convenience function that sets the PHY up for a
// standard serial port.
func NewSerialSimple(portName string) (*PHY, error) {
	options := serial.PortOptions{
		PortName:      portName,
		FlowControl:   false,
		InterfaceRate: 9600,
	}

	port, err := serial.Open(&options)
	if err != nil {
		return nil, err
	}

	phy := PHY{
		Port:               port,
		TXDisableScrambler: false,
		TXSendBreak:        port.DoBreak,
	}

	/* Test if sending break works */
	err = phy.TXSendBreak(10 * time.Millisecond)

	/* Workaround for serial ports that cannot send a break */
	if err != nil {
		phy.TXSendBreak = func(d time.Duration) error {
			port.SetInterfaceRate(300)
			cd := 70 * time.Millisecond
			for t := time.Duration(0); t < d; t += cd {
				port.Write([]byte{0})
				time.Sleep(cd)
			}
			port.SetInterfaceRate(options.InterfaceRate)
			return nil
		}
	}

	return &phy, nil
}
