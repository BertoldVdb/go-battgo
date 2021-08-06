package main

import (
	"encoding/json"
	"flag"
	"log"

	"github.com/BertoldVdb/go-battgo/controller"
	"github.com/BertoldVdb/go-battgo/controller/functions/battery"
	"github.com/BertoldVdb/go-battgo/phy"
)

func main() {
	port := flag.String("port", "/dev/ttyUSB0", "Serial port to use")
	dev := flag.Int("devices", -1, "Number of devices on bus")
	flag.Parse()

	phy, err := phy.NewSerialSimple(*port)
	if err != nil {
		log.Fatalln("Could not create PHY", err)
	}
	defer phy.Close()

	updateChan := make(chan (*battery.DeviceBattery), 1)
	go func() {
		for {
			dev := <-updateChan

			dev.Data.Lock()
			b, err := json.MarshalIndent(&dev.Data, "", "  ")
			dev.Data.Unlock()

			if err == nil {
				log.Println(string(b))
			}
		}
	}()

	b := controller.New(phy, *dev, func(newDevice *controller.BusDevice) controller.FunctionalDevice {
		return battery.New(newDevice, updateChan)
	})

	log.Fatalln(b.Run())
}
