package main

import "log"

func main() {
	if err := runRecorderWindow(); err != nil {
		log.Fatal(err)
	}
}
