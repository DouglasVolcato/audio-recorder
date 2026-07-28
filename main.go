package main

import (
	"fmt"
	"log"
	"runtime"

	"github.com/douglasvolcato/audio-recorder/devices"
)

func main() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	inputDevice, outputDevice, err, cleanup := devices.GetDevices()

	if err != nil {
		log.Fatal(err)
	}

	defer cleanup()

	inputName, _ := devices.GetDeviceName(inputDevice)
	outputName, _ := devices.GetDeviceName(outputDevice)

	fmt.Println("Microfone:", inputName)
	fmt.Println("Saída:", outputName)
	fmt.Println("Gravando por 10 segundos...")

	err = devices.Record10Seconds(
		inputDevice,
		outputDevice,
		"gravacao.wav",
	)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("Arquivo criado: gravacao.wav")
}
