package main

import (
	"fmt"
	"go1090/mode_s"
	"go1090/rtl_adsb"
	"log"
	"sort"
	"time"

	"github.com/awesome-gocui/gocui"
	. "github.com/logrusorgru/aurora"
)

type Context struct {
	decoder *mode_s.Decoder
	sky     *mode_s.Sky
}

func CreateContext() *Context {
	return &Context{
		decoder: &mode_s.Decoder{},
		sky:     mode_s.NewSky(),
	}
}

func (ctx *Context) update(g *gocui.Gui) error {
	// update time and aircraft count
	s, _ := g.View("status")
	s.Clear()
	fmt.Fprintf(s, " A/C: %02d  LAST UPDATE: %s\n",
		Green(ctx.sky.AircraftCount()),
		Bold(Green(time.Now().Format("2006-01-02 15:04:05"))))

	l, _ := g.View("list")
	l.Clear()

	// display aircraft list
	fmt.Fprintln(l, " ICAO ADDR    FLIGHT     ALT    SPD    HDG     LAT     LON  SEEN")
	fmt.Fprintln(l, " ===================================================================")

	aircrafts := ctx.sky.Aircrafts()
	addrs := make([]uint32, 0, len(aircrafts))
	for addr := range aircrafts {
		addrs = append(addrs, addr)
	}
	sort.Slice(addrs, func(i, j int) bool { return addrs[i] < addrs[j] })

	for _, addr := range addrs {
		ac := aircrafts[addr]
		fmt.Fprintln(l, Sprintf(Yellow(" %6s       %9s  %-5d  %-5d  %-3d  %6.2f  %6.2f  %s"),
			ac.HexAddr,
			ac.Flight,
			ac.Altitude,
			ac.Speed,
			ac.Track,
			ac.Latitude,
			ac.Longitude,
			ac.Seen.Format("15:04:05")))
	}

	return nil
}

func main() {
	// init ui
	g, err := gocui.NewGui(gocui.OutputNormal, false)
	if err != nil {
		log.Panicln(err)
	}

	defer g.Close()

	g.SetManagerFunc(layout)

	if err := g.SetKeybinding("", gocui.KeyCtrlC, gocui.ModNone, quit); err != nil {
		log.Panicln(err)
	}

	// init decoder and sky
	ctx := CreateContext()
	ctx.decoder.Init()

	// start receive
	handler := func(rcv rtl_adsb.ADSBMsg) {
		msg := mode_s.ModeSMessage{}
		ctx.decoder.DecodeModesMessage(&msg, rcv[:])

		ctx.sky.UpdateData(&msg)
		g.Update(ctx.update)
	}

	stopFunc, e := rtl_adsb.StartReceive("rtl_adsb.exe", handler)

	if e != nil {
		log.Panicln("error: ", e)
	}

	//
	go func() {
		for ; ; <-time.Tick(time.Second * 1) {
			ctx.sky.RemoveStaleAircrafts()
			g.Update(ctx.update)
		}
	}()

	if err := g.MainLoop(); err != nil && !gocui.IsQuit(err) {
		log.Panicln(err)
	}

	stopFunc()
}

func layout(g *gocui.Gui) error {
	// layout
	const maxX = 80
	_, maxY := g.Size()

	v, _ := g.SetView("status", 0, 0, maxX-2, 2, 0)
	v.Title = " STATUS "
	fmt.Fprintln(v, " A/C: --  LAST UPDATE: 0000-00-00 00:00:00")

	v, _ = g.SetView("list", 0, 3, maxX-2, maxY-1, 0)
	v.Title = " A/C "
	return nil
}

func quit(g *gocui.Gui, v *gocui.View) error {
	return gocui.ErrQuit
}
