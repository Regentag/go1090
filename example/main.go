// This example program outputs an ADS-B message to the console
// until Ctrl+C is pressed.
// 2018. 11. 06.

package main

import (
	"fmt"
	"github.com/Regentag/go1090"
	"os"
	"os/signal"
	"syscall"
)

func printADS_B(msg *go1090.ADSBMsg) {
	// print ads-b message (Downlink Format 17 or 18)
	if msg.DF == 17 || msg.DF == 18 {
		msg.PrintMessage()
	}
}

func main() {
	sigs := make(chan os.Signal, 1)
	done := make(chan bool, 1)

	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigs
		fmt.Println()
		fmt.Println(sig)
		done <- true
	}()

	stopFunc, e := go1090.StartReceive(
		"C:\\rtl-sdr-release\\x64\\rtl_adsb.exe", // path to rtl_adsb.exe (included in RTL-SDR package.)
		printADS_B)                               // message handling functino

	if e != nil {
		fmt.Println("error: ", e)
	}

	fmt.Println("awaiting signal")
	<-done
	stopFunc()
	fmt.Println("exiting")
}
