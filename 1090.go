/*
Copyright (c) 2018 Ham, Yeongtaek <yeongtaek.ham@gmail.com>.

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
*/

package go1090

import (
	"bufio"
	"fmt"
	"log"
	"os/exec"
	"strconv"
)

// ADSBMsg : ADS-B (Mode S) message.
type ADSBMsg struct {
	DF     uint8    // Downlink Format
	CA     uint8    // Capability (additional identifier)
	ICAO24 [3]uint8 // ICAO aircraft address
	DATA   [7]uint8 // Data or Type code [TC]
	PI     [3]uint8 // Parity / Interrogator ID
}

// PrintMessage :
func (m *ADSBMsg) PrintMessage() {
	fmt.Printf("DF: %d CA: %d ICAO24: %02X%02X%02X ",
		m.DF, m.CA,
		m.ICAO24[0], m.ICAO24[1], m.ICAO24[2])
	fmt.Printf("DATA: %02X%02X%02X%02X%02X%02X%02X ",
		m.DATA[0], m.DATA[1], m.DATA[2], m.DATA[3],
		m.DATA[4], m.DATA[5], m.DATA[6])
	fmt.Printf("PI: %02X%02X%02X\n",
		m.PI[0], m.PI[1], m.PI[2])
}

// MessageHandler is function for handling ADS-B Message.
type MessageHandler func(*ADSBMsg)

// StartReceive function.
func StartReceive(execPath string, handler MessageHandler) (func(), error) {
	fmt.Println("Exec path: ", execPath)
	cmd := exec.Command(execPath)
	stdout, err := cmd.StdoutPipe()

	if err != nil {
		return nil, fmt.Errorf("RTL-ADSB Error: %s", err.Error())
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("RTL-ADSB Error: %s", err.Error())
	}

	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			line := scanner.Text()

			// ADS-B messages are starts with '*' charactor.
			if len(line) > 0 && line[0] == '*' {
				m := parseADSB(line)
				if m != nil {
					handler(m)
				}
			}
		}
		cmd.Wait()
	}()
	return func() {
		cmd.Process.Kill()
		log.Println("RTL-ADSB killed by caller.")
	}, nil
}

// Parse ADS-B data.
// See: https://mode-s.org/decode/adsb/introduction.html
func parseADSB(hexstr string) *ADSBMsg {
	log.Println("MSG: ", hexstr)

	if isValidMsgText(hexstr) {
		var bin [14]uint8
		bin[0] = parseHex(hexstr[1:3])
		bin[1] = parseHex(hexstr[3:5])
		bin[2] = parseHex(hexstr[5:7])
		bin[3] = parseHex(hexstr[7:9])
		bin[4] = parseHex(hexstr[9:11])
		bin[5] = parseHex(hexstr[11:13])
		bin[6] = parseHex(hexstr[13:15])
		bin[7] = parseHex(hexstr[15:17])
		bin[8] = parseHex(hexstr[17:19])
		bin[9] = parseHex(hexstr[19:21])
		bin[10] = parseHex(hexstr[21:23])
		bin[11] = parseHex(hexstr[23:25])
		bin[12] = parseHex(hexstr[25:27])
		bin[13] = parseHex(hexstr[27:29])

		msg := new(ADSBMsg)
		msg.DF = bin[0] >> 3
		msg.CA = bin[0] & 0x7
		msg.ICAO24[0] = bin[1]
		msg.ICAO24[1] = bin[2]
		msg.ICAO24[2] = bin[3]
		msg.DATA[0] = bin[4]
		msg.DATA[1] = bin[5]
		msg.DATA[2] = bin[6]
		msg.DATA[3] = bin[7]
		msg.DATA[4] = bin[8]
		msg.DATA[5] = bin[9]
		msg.DATA[6] = bin[10]
		msg.PI[0] = bin[11]
		msg.PI[1] = bin[12]
		msg.PI[2] = bin[13]

		return msg
	}

	return nil
}

func parseHex(hexstr string) uint8 {
	n, _ := strconv.ParseUint(hexstr, 16, 8)
	return uint8(n)
}

// message format (from rtl_adsb.exe):
//   *112233445566778899AABBCCDDEE;
func isValidMsgText(hexstr string) bool {
	if len(hexstr) != 30 {
		// rtl_adsb
		return false
	}

	if hexstr[0] != '*' || hexstr[29] != ';' {
		return false
	}

	return true
}
