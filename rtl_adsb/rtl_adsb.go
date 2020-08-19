package rtl_adsb

import (
	"bufio"
	"fmt"
	"os/exec"
	"strconv"
)

type ADSBMsg [14]byte

// MessageHandler is function for handling ADS-B Message.
type MessageHandler func(ADSBMsg)

// StartReceive function.
func StartReceive(execPath string, handler MessageHandler) (func(), error) {
	cmd := exec.Command(execPath)
	stdout, err := cmd.StdoutPipe()

	if err != nil {
		return nil, fmt.Errorf("RTL-ADSB error: %s", err.Error())
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("RTL-ADSB error: %s", err.Error())
	}

	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			line := scanner.Text()

			m := parseADSB(line)
			if m != nil {
				handler(*m)
			}
		}
		cmd.Wait()
	}()
	return func() {
		cmd.Process.Kill()
	}, nil
}

// Parse ADS-B data.
// See: https://mode-s.org/decode/adsb/introduction.html
func parseADSB(hexstr string) *ADSBMsg {
	if isValidMsgText(hexstr) {
		var bin ADSBMsg
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

		return &bin
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
		return false
	}

	if hexstr[0] != '*' || hexstr[29] != ';' {
		return false
	}

	return true
}
