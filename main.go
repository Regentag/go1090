package main

import (
	"fmt"
	"go1090/mode_s"
	"go1090/rtl_adsb"
	"log"
	"time"

	"github.com/awesome-gocui/gocui"
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
	fmt.Fprintf(s, " Aircrafts: %02d  Last Update: %s\n",
		ctx.sky.AircraftCount(),
		time.Now().Format("2006-01-02 15:04:05"))

	l, _ := g.View("list")
	l.Clear()
	ctx.sky.PrintAircrafts(l)
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

	handler := func(rcv rtl_adsb.ADSBMsg) {
		msg := mode_s.ModeSMessage{}
		ctx.decoder.DecodeModesMessage(&msg, rcv[:])

		ctx.sky.UpdateData(&msg)
		ctx.sky.RemoveStaleAircrafts()

		g.Update(ctx.update)
	}

	// start receive
	stopFunc, e := rtl_adsb.StartReceive("rtl_adsb.exe", handler)

	if e != nil {
		log.Panicln("error: ", e)
	}

	if err := g.MainLoop(); err != nil && !gocui.IsQuit(err) {
		log.Panicln(err)
	}

	stopFunc()
}

func layout(g *gocui.Gui) error {
	// colour settings
	g.BgColor = gocui.ColorCyan
	g.FgColor = gocui.ColorBlack

	// layout
	maxX, maxY := g.Size()

	v, _ := g.SetView("status", 0, 0, maxX-2, 2, 0)
	v.BgColor = gocui.ColorWhite
	fmt.Fprintln(v, " Aircrafts: --  Last Update: 0000-00-00 00:00:00")

	v, _ = g.SetView("list", 0, 3, maxX-2, maxY-1, 0)
	v.Title = "[ Aircrafts ]"
	return nil
}

func quit(g *gocui.Gui, v *gocui.View) error {
	return gocui.ErrQuit
}
