package mode_s

import (
	"fmt"
	"math"
	"time"

	"github.com/patrickmn/go-cache"
)

const MODES_PREAMBLE_US = 8 /* microseconds */
const MODES_LONG_MSG_BITS = 112
const MODES_SHORT_MSG_BITS = 56
const MODES_FULL_LEN = (MODES_PREAMBLE_US + MODES_LONG_MSG_BITS)
const MODES_LONG_MSG_BYTES = (112 / 8)
const MODES_SHORT_MSG_BYTES = (56 / 8)

const (
	MODES_ICAO_CACHE_TTL = 60 /* Time to live of cached addresses. */
)

const (
	MODES_UNIT_FEET   = 0
	MODES_UNIT_METERS = 1
)

const (
	East = 0
	West = 1

	North = 0
	South = 1
)

type Decoder struct {
	/* Internal state */
	icao_cache *cache.Cache /* Recently seen ICAO addresses cache. */

	/* Configuration */
	fix_errors       bool /* Single bit error correction if true. */
	check_crc        bool /* Only display messages with good CRC. */
	interactive      int  /* Interactive mode */
	interactive_rows int  /* Interactive mode: max number of rows. */
	metric           int  /* Use metric units. */
	aggressive       bool /* Aggressive detection algorithm. */
}

/* The struct we use to store information about a decoded message. */
type ModeSMessage struct {
	/* Generic fields */
	msg             []byte /* Binary message. */
	msgbits         int    /* Number of bits in message */
	msgtype         int    /* Downlink format # */
	crcok           bool   /* True if CRC was valid */
	crc             uint32 /* Message CRC */
	errorbit        int    /* Bit corrected. -1 if no bit corrected. */
	aa1, aa2, aa3   uint32 /* ICAO Address bytes 1 2 and 3 */
	phase_corrected int    /* True if phase correction was applied. */

	/* DF 11 */
	ca int /* Responder capabilities. */

	/* DF 17 */
	metype           int /* Extended squitter message type. */
	mesub            int /* Extended squitter message subtype. */
	heading_is_valid int
	heading          int
	aircraft_type    int
	fflag            int     /* 1 = Odd, 0 = Even CPR message. */
	tflag            int     /* UTC synchronized? */
	raw_latitude     int     /* Non decoded latitude */
	raw_longitude    int     /* Non decoded longitude */
	flight           [9]rune /* 8 chars flight number. */
	ew_dir           int     /* 0 = East, 1 = West. */
	ew_velocity      int     /* E/W velocity. */
	ns_dir           int     /* 0 = North, 1 = South. */
	ns_velocity      int     /* N/S velocity. */
	vert_rate_source int     /* Vertical rate source. */
	vert_rate_sign   int     /* Vertical rate sign. */
	vert_rate        int     /* Vertical rate. */
	velocity         int     /* Computed from EW and NS velocity. */

	/* DF4, DF5, DF20, DF21 */
	fs       int /* Flight status for DF4,5,20,21 */
	dr       int /* Request extraction of downlink request. */
	um       int /* Request extraction of downlink request. */
	identity int /* 13 bits identity (Squawk). */

	/* Fields used by multiple message types. */
	altitude int
	unit     int
}

/* Parity table for MODE S Messages.
 * The table contains 112 elements, every element corresponds to a bit set
 * in the message, starting from the first bit of actual data after the
 * preamble.
 *
 * For messages of 112 bit, the whole table is used.
 * For messages of 56 bits only the last 56 elements are used.
 *
 * The algorithm is as simple as xoring all the elements in this table
 * for which the corresponding bit on the message is set to 1.
 *
 * The latest 24 elements in this table are set to 0 as the checksum at the
 * end of the message should not affect the computation.
 *
 * Note: this function can be used with DF11 and DF17, other modes have
 * the CRC xored with the sender address as they are reply to interrogations,
 * but a casual listener can't split the address from the checksum.
 */
func modesChecksumTable() []uint32 {
	return []uint32{
		0x3935ea, 0x1c9af5, 0xf1b77e, 0x78dbbf, 0xc397db, 0x9e31e9, 0xb0e2f0, 0x587178,
		0x2c38bc, 0x161c5e, 0x0b0e2f, 0xfa7d13, 0x82c48d, 0xbe9842, 0x5f4c21, 0xd05c14,
		0x682e0a, 0x341705, 0xe5f186, 0x72f8c3, 0xc68665, 0x9cb936, 0x4e5c9b, 0xd8d449,
		0x939020, 0x49c810, 0x24e408, 0x127204, 0x093902, 0x049c81, 0xfdb444, 0x7eda22,
		0x3f6d11, 0xe04c8c, 0x702646, 0x381323, 0xe3f395, 0x8e03ce, 0x4701e7, 0xdc7af7,
		0x91c77f, 0xb719bb, 0xa476d9, 0xadc168, 0x56e0b4, 0x2b705a, 0x15b82d, 0xf52612,
		0x7a9309, 0xc2b380, 0x6159c0, 0x30ace0, 0x185670, 0x0c2b38, 0x06159c, 0x030ace,
		0x018567, 0xff38b7, 0x80665f, 0xbfc92b, 0xa01e91, 0xaff54c, 0x57faa6, 0x2bfd53,
		0xea04ad, 0x8af852, 0x457c29, 0xdd4410, 0x6ea208, 0x375104, 0x1ba882, 0x0dd441,
		0xf91024, 0x7c8812, 0x3e4409, 0xe0d800, 0x706c00, 0x383600, 0x1c1b00, 0x0e0d80,
		0x0706c0, 0x038360, 0x01c1b0, 0x00e0d8, 0x00706c, 0x003836, 0x001c1b, 0xfff409,
		0x000000, 0x000000, 0x000000, 0x000000, 0x000000, 0x000000, 0x000000, 0x000000,
		0x000000, 0x000000, 0x000000, 0x000000, 0x000000, 0x000000, 0x000000, 0x000000,
		0x000000, 0x000000, 0x000000, 0x000000, 0x000000, 0x000000, 0x000000, 0x000000,
	}
}

func modesChecksum(msg []byte, bits int) uint32 {
	var crc uint32 = 0
	var offset int
	if bits == 112 {
		offset = 0
	} else {
		offset = 112 - 56
	}

	for j := 0; j < bits; j++ {
		s_byte := j / 8
		var s_bit byte = byte(j) % 8
		var s_bitmask byte = 1 << (7 - s_bit)

		/* If bit is set, xor with corresponding table entry. */
		if (msg[s_byte] & s_bitmask) != 0 {
			crc ^= modesChecksumTable()[j+offset]
		}
	}
	return crc /* 24 bit checksum. */
}

/* Given the Downlink Format (DF) of the message, return the message length
 * in bits. */
func modesMessageLenByType(msgType int) int {
	switch msgType {
	case 16, 17, 19, 20, 21:
		return MODES_LONG_MSG_BITS
	default:
		return MODES_SHORT_MSG_BITS

	}
}

/* Try to fix single bit errors using the checksum. On success modifies
 * the original buffer with the fixed version, and returns the position
 * of the error bit. Otherwise if fixing failed -1 is returned. */
func fixSingleBitErrors(msg []byte, bits int) int {
	msgBytes := bits / 8
	var aux []byte = make([]byte, msgBytes)

	for j := 0; j < bits; j++ {
		s_byte := j / 8
		var bitmask byte = 1 << (7 - (j % 8))
		var crc1, crc2 uint32

		copy(aux, msg)
		aux[s_byte] ^= bitmask /* Flip j-th bit. */

		crc1 = (uint32(aux[msgBytes-3]) << 16) |
			(uint32(aux[msgBytes-2]) << 8) |
			uint32(aux[msgBytes-1])
		crc2 = modesChecksum(aux, bits)

		if crc1 == crc2 {
			/* The error is fixed. Overwrite the original buffer with
			 * the corrected sequence, and returns the error bit
			 * position. */
			copy(msg, aux)
			return j
		}
	}

	return -1
}

/* Similar to fixSingleBitErrors() but try every possible two bit combination.
 * This is very slow and should be tried only against DF17 messages that
 * don't pass the checksum, and only in Aggressive Mode. */
func fixTwoBitsErrors(msg []byte, bits int) int {
	msgBytes := bits / 8
	var aux []byte = make([]byte, msgBytes)

	for j := 0; j < bits; j++ {
		byte1 := j / 8
		var bitmask1 byte = 1 << (7 - (j % 8))

		/* Don't check the same pairs multiple times, so i starts from j+1 */
		for i := j + 1; i < bits; i++ {
			byte2 := i / 8
			var bitmask2 byte = 1 << (7 - (i % 8))
			var crc1, crc2 uint32

			copy(aux, msg)

			aux[byte1] ^= bitmask1 /* Flip j-th bit. */
			aux[byte2] ^= bitmask2 /* Flip i-th bit. */

			crc1 = (uint32(aux[msgBytes-3]) << 16) |
				(uint32(aux[msgBytes-2]) << 8) |
				uint32(aux[msgBytes-1])
			crc2 = modesChecksum(aux, bits)

			if crc1 == crc2 {
				/* The error is fixed. Overwrite the original buffer with
				 * the corrected sequence, and returns the error bit
				 * position. */
				copy(msg, aux)

				/* We return the two bits as a 16 bit integer by shifting
				 * 'i' on the left. This is possible since 'i' will always
				 * be non-zero because i starts from j+1. */
				return j | (i << 8)
			}
		}
	}

	return -1
}

func (self *Decoder) modesInitConfig() {
	self.fix_errors = true
	self.check_crc = true
	self.interactive = 0
	self.aggressive = false
}

func (self *Decoder) Init() {
	self.modesInitConfig()

	/* Allocate the ICAO address cache. */
	self.icao_cache = cache.New(MODES_ICAO_CACHE_TTL*time.Second, 10*time.Second)
}

/* Add the specified entry to the cache of recently seen ICAO addresses.
 * Note that we also add a timestamp so that we can make sure that the
 * entry is only valid for MODES_ICAO_CACHE_TTL seconds. */
func (self *Decoder) addRecentlySeenICAOAddr(addr uint32) {
	self.icao_cache.SetDefault(fmt.Sprint(addr), addr)
}

/* Returns true if the specified ICAO address was seen in a DF format with
 * proper checksum (not xored with address) no more than MODES_ICAO_CACHE_TTL
 * seconds ago. Otherwise returns 0. */
func (self *Decoder) icaoAddressWasRecentlySeen(addr uint32) bool {
	_, found := self.icao_cache.Get(fmt.Sprint(addr))
	return found
}

/* If the message type has the checksum xored with the ICAO address, try to
 * brute force it using a list of recently seen ICAO addresses.
 *
 * Do this in a brute-force fashion by xoring the predicted CRC with
 * the address XOR checksum field in the message. This will recover the
 * address: if we found it in our cache, we can assume the message is ok.
 *
 * This function expects mm->msgtype and mm->msgbits to be correctly
 * populated by the caller.
 *
 * On success the correct ICAO address is stored in the modesMessage
 * structure in the aa3, aa2, and aa1 fiedls.
 *
 * If the function successfully recovers a message with a correct checksum
 * it returns nil. Otherwise error is returned. */
func (self *Decoder) bruteForceAP(msg []byte, mm *ModeSMessage) error {

	msgtype := mm.msgtype
	msgbits := mm.msgbits

	switch msgtype {
	case 0, /* Short air surveillance */
		4,  /* Surveillance, altitude reply */
		5,  /* Surveillance, identity reply */
		16, /* Long Air-Air survillance */
		20, /* Comm-A, altitude request */
		21, /* Comm-A, identity request */
		24: /* Comm-C ELM */

		var aux []byte = make([]byte, MODES_LONG_MSG_BYTES)

		var addr uint32
		var crc uint32
		lastbyte := (msgbits / 8) - 1

		/* Work on a copy. */
		copy(aux, msg)

		/* Compute the CRC of the message and XOR it with the AP field
		 * so that we recover the address, because:
		 *
		 * (ADDR xor CRC) xor CRC = ADDR. */
		crc = modesChecksum(aux, msgbits)
		aux[lastbyte] ^= byte(crc & 0xff)
		aux[lastbyte-1] ^= byte((crc >> 8) & 0xff)
		aux[lastbyte-2] ^= byte((crc >> 16) & 0xff)

		/* If the obtained address exists in our cache we consider
		 * the message valid. */
		addr = uint32(aux[lastbyte]) | uint32(aux[lastbyte-1])<<8 | uint32(aux[lastbyte-2])<<16
		if self.icaoAddressWasRecentlySeen(addr) {
			mm.aa1 = uint32(aux[lastbyte-2])
			mm.aa2 = uint32(aux[lastbyte-1])
			mm.aa3 = uint32(aux[lastbyte])

			return nil
		}
	}

	return fmt.Errorf("can't recover message")
}

/* Decode the 13 bit AC altitude field (in DF 20 and others).
 * Returns the altitude, and set 'unit' to either MODES_UNIT_METERS
 * or MDOES_UNIT_FEETS. */
func decodeAC13Field(msg []byte, unit int) (altitude, newUnit int) {
	m_bit := msg[3] & (1 << 6)
	q_bit := msg[3] & (1 << 4)

	if m_bit == 0 {
		newUnit = MODES_UNIT_FEET
		if q_bit != 0 {
			/* N is the 11 bit integer resulting from the removal of bit
			 * Q and M */
			n := ((msg[2] & 31) << 6) |
				((msg[3] & 0x80) >> 2) |
				((msg[3] & 0x20) >> 1) |
				(msg[3] & 15)
			/* The final altitude is due to the resulting number multiplied
			 * by 25, minus 1000. */
			altitude = int(n)*25 - 1000
		} else {
			altitude = 0
			/* TODO: Implement altitude where Q=0 and M=0 */
		}
	} else {
		newUnit = MODES_UNIT_METERS
		altitude = 0
		/* TODO: Implement altitude when meter unit is selected. */
	}

	return
}

/* Decode the 12 bit AC altitude field (in DF 17 and others).
 * Returns the altitude or 0 if it can't be decoded. */
func decodeAC12Field(msg []byte, unit int) (altitude, newUnit int) {
	q_bit := msg[5] & 1

	if q_bit != 0 {
		/* N is the 11 bit integer resulting from the removal of bit
		 * Q */
		newUnit = MODES_UNIT_FEET
		n := ((msg[5] >> 1) << 4) | ((msg[6] & 0xF0) >> 4)
		/* The final altitude is due to the resulting number multiplied
		 * by 25, minus 1000. */
		altitude = int(n)*25 - 1000
	} else {
		newUnit = unit
		altitude = 0
	}

	return
}

/* Capability table. */
func caStr() []string {
	return []string{
		/* 0 */ "Level 1 (Survillance Only)",
		/* 1 */ "Level 2 (DF0,4,5,11)",
		/* 2 */ "Level 3 (DF0,4,5,11,20,21)",
		/* 3 */ "Level 4 (DF0,4,5,11,20,21,24)",
		/* 4 */ "Level 2+3+4 (DF0,4,5,11,20,21,24,code7 - is on ground)",
		/* 5 */ "Level 2+3+4 (DF0,4,5,11,20,21,24,code7 - is on airborne)",
		/* 6 */ "Level 2+3+4 (DF0,4,5,11,20,21,24,code7)",
		/* 7 */ "Level 7 ???",
	}
}

/* Flight status table. */
func fsStr() []string {
	return []string{
		/* 0 */ "Normal, Airborne",
		/* 1 */ "Normal, On the ground",
		/* 2 */ "ALERT,  Airborne",
		/* 3 */ "ALERT,  On the ground",
		/* 4 */ "ALERT & Special Position Identification. Airborne or Ground",
		/* 5 */ "Special Position Identification. Airborne or Ground",
		/* 6 */ "Value 6 is not assigned",
		/* 7 */ "Value 7 is not assigned",
	}
}

func getMEDescription(metype, mesub int) string {
	switch {
	case metype >= 1 && metype <= 4:
		return "Aircraft Identification and Category"
	case metype >= 5 && metype <= 8:
		return "Surface Position"
	case metype >= 9 && metype <= 18:
		return "Airborne Position (Baro Altitude)"
	case metype == 19 && mesub >= 1 && mesub <= 4:
		return "Airborne Velocity"
	case metype >= 20 && metype <= 22:
		return "Airborne Position (GNSS Height)"
	case metype == 23 && mesub == 0:
		return "Test Message"
	case metype == 24 && mesub == 1:
		return "Surface System Status"
	case metype == 28 && mesub == 1:
		return "Extended Squitter Aircraft Status (Emergency)"
	case metype == 28 && mesub == 2:
		return "Extended Squitter Aircraft Status (1090ES TCAS RA)"
	case metype == 29 && (mesub == 0 || mesub == 1):
		return "Target State and Status Message"
	case metype == 31 && (mesub == 0 || mesub == 1):
		return "Aircraft Operational Status Message"
	}

	return "Unknown"
}

/* Decode a raw Mode S message demodulated as a stream of bytes by
 * detectModeS(), and split it into fields populating a modesMessage
 * structure. */
func (self *Decoder) DecodeModesMessage(mm *ModeSMessage, msg []byte) {
	var crc2 uint32 /* Computed CRC, used to verify the message CRC. */
	var ais_charset []rune = []rune("?ABCDEFGHIJKLMNOPQRSTUVWXYZ????? ???????????????0123456789??????")

	/* Work on our local copy */
	mm.msg = make([]byte, len(msg))
	copy(mm.msg, msg)

	msg = mm.msg

	/* Get the message type ASAP as other operations depend on this */
	mm.msgtype = int(msg[0]) >> 3 /* Downlink Format */
	mm.msgbits = modesMessageLenByType(mm.msgtype)

	/* CRC is always the last three bytes. */
	mm.crc = (uint32(msg[(mm.msgbits/8)-3]) << 16) |
		(uint32(msg[(mm.msgbits/8)-2]) << 8) |
		uint32(msg[(mm.msgbits/8)-1])
	crc2 = modesChecksum(msg, mm.msgbits)

	/* Check CRC and fix single bit errors using the CRC when
	 * possible (DF 11 and 17). */
	mm.errorbit = -1 /* No error */
	mm.crcok = (mm.crc == crc2)

	if !mm.crcok && self.fix_errors && (mm.msgtype == 11 || mm.msgtype == 17) {
		if mm.errorbit = fixSingleBitErrors(msg, mm.msgbits); mm.errorbit != -1 {
			mm.crc = modesChecksum(msg, mm.msgbits)
			mm.crcok = true
		} else if mm.errorbit = fixTwoBitsErrors(msg, mm.msgbits); self.aggressive && (mm.msgtype == 17) && mm.errorbit != -1 {
			mm.crc = modesChecksum(msg, mm.msgbits)
			mm.crcok = true
		}
	}

	/* Note that most of the other computation happens *after* we fix
	 * the single bit errors, otherwise we would need to recompute the
	 * fields again. */
	mm.ca = int(msg[0]) & 7 /* Responder capabilities. */

	/* ICAO address */
	mm.aa1 = uint32(msg[1])
	mm.aa2 = uint32(msg[2])
	mm.aa3 = uint32(msg[3])

	/* DF 17 type (assuming this is a DF17, otherwise not used) */
	mm.metype = int(msg[4]) >> 3 /* Extended squitter message type. */
	mm.mesub = int(msg[4]) & 7   /* Extended squitter message subtype. */

	/* Fields for DF4,5,20,21 */
	mm.fs = int(msg[0]) & 7            /* Flight status for DF4,5,20,21 */
	mm.dr = int(msg[1]) >> 3 & 31      /* Request extraction of downlink request. */
	mm.um = ((int(msg[1]) & 7) << 3) | /* Request extraction of downlink request. */
		int(msg[2])>>5

	/* In the squawk (identity) field bits are interleaved like that
	 * (message bit 20 to bit 32):
	 *
	 * C1-A1-C2-A2-C4-A4-ZERO-B1-D1-B2-D2-B4-D4
	 *
	 * So every group of three bits A, B, C, D represent an integer
	 * from 0 to 7.
	 *
	 * The actual meaning is just 4 octal numbers, but we convert it
	 * into a base ten number tha happens to represent the four
	 * octal numbers.
	 *
	 * For more info: http://en.wikipedia.org/wiki/Gillham_code */
	{
		var a, b, c, d byte

		a = ((msg[3] & 0x80) >> 5) |
			((msg[2] & 0x02) >> 0) |
			((msg[2] & 0x08) >> 3)
		b = ((msg[3] & 0x02) << 1) |
			((msg[3] & 0x08) >> 2) |
			((msg[3] & 0x20) >> 5)
		c = ((msg[2] & 0x01) << 2) |
			((msg[2] & 0x04) >> 1) |
			((msg[2] & 0x10) >> 4)
		d = ((msg[3] & 0x01) << 2) |
			((msg[3] & 0x04) >> 1) |
			((msg[3] & 0x10) >> 4)
		mm.identity = int(a)*1000 + int(b)*100 + int(c)*10 + int(d)
	}

	/* DF 11 & 17: try to populate our ICAO addresses whitelist.
	 * DFs with an AP field (xored addr and crc), try to decode it. */
	if mm.msgtype != 11 && mm.msgtype != 17 {
		/* Check if we can check the checksum for the Downlink Formats where
		 * the checksum is xored with the aircraft ICAO address. We try to
		 * brute force it using a list of recently seen aircraft addresses. */
		if self.bruteForceAP(msg, mm) == nil {
			/* We recovered the message, mark the checksum as valid. */
			mm.crcok = true
		} else {
			mm.crcok = false
		}
	} else {
		/* If this is DF 11 or DF 17 and the checksum was ok,
		 * we can add this address to the list of recently seen
		 * addresses. */
		if mm.crcok && mm.errorbit == -1 {
			var addr uint32 = (mm.aa1 << 16) | (mm.aa2 << 8) | mm.aa3
			self.addRecentlySeenICAOAddr(addr)
		}
	}

	/* Decode 13 bit altitude for DF0, DF4, DF16, DF20 */
	if mm.msgtype == 0 || mm.msgtype == 4 ||
		mm.msgtype == 16 || mm.msgtype == 20 {
		mm.altitude, mm.unit = decodeAC13Field(msg, mm.unit)
	}

	/* Decode extended squitter specific stuff. */
	if mm.msgtype == 17 {
		/* Decode the extended squitter message. */

		if mm.metype >= 1 && mm.metype <= 4 {
			/* Aircraft Identification and Category */
			mm.aircraft_type = mm.metype - 1

			mm.flight[0] = ais_charset[msg[5]>>2]
			mm.flight[1] = ais_charset[((msg[5]&3)<<4)|(msg[6]>>4)]
			mm.flight[2] = ais_charset[((msg[6]&15)<<2)|(msg[7]>>6)]
			mm.flight[3] = ais_charset[msg[7]&63]
			mm.flight[4] = ais_charset[msg[8]>>2]
			mm.flight[5] = ais_charset[((msg[8]&3)<<4)|(msg[9]>>4)]
			mm.flight[6] = ais_charset[((msg[9]&15)<<2)|(msg[10]>>6)]
			mm.flight[7] = ais_charset[msg[10]&63]
			mm.flight[8] = 0
		} else if mm.metype >= 9 && mm.metype <= 18 {
			/* Airborne position Message */
			mm.fflag = int(msg[6]) & (1 << 2)
			mm.tflag = int(msg[6]) & (1 << 3)
			mm.altitude, mm.unit = decodeAC12Field(msg, mm.unit)
			mm.raw_latitude = ((int(msg[6]) & 3) << 15) |
				(int(msg[7]) << 7) |
				(int(msg[8]) >> 1)
			mm.raw_longitude = ((int(msg[8]) & 1) << 16) |
				(int(msg[9]) << 8) |
				int(msg[10])
		} else if mm.metype == 19 && mm.mesub >= 1 && mm.mesub <= 4 {
			/* Airborne Velocity Message */
			if mm.mesub == 1 || mm.mesub == 2 {
				mm.ew_dir = (int(msg[5]) & 4) >> 2
				mm.ew_velocity = ((int(msg[5]) & 3) << 8) | int(msg[6])
				mm.ns_dir = (int(msg[7]) & 0x80) >> 7
				mm.ns_velocity = ((int(msg[7]) & 0x7f) << 3) | ((int(msg[8]) & 0xe0) >> 5)
				mm.vert_rate_source = (int(msg[8]) & 0x10) >> 4
				mm.vert_rate_sign = (int(msg[8]) & 0x8) >> 3
				mm.vert_rate = ((int(msg[8]) & 7) << 6) | ((int(msg[9]) & 0xfc) >> 2)

				/* Compute velocity and angle from the two speed
				 * components. */
				mm.velocity = int(math.Sqrt(float64(mm.ns_velocity*mm.ns_velocity + mm.ew_velocity*mm.ew_velocity)))
				if mm.velocity != 0 {
					ewv := mm.ew_velocity
					nsv := mm.ns_velocity
					var heading float64

					if mm.ew_dir == West {
						ewv *= -1
					}
					if mm.ns_dir == South {
						nsv *= -1
					}

					heading = math.Atan2(float64(ewv), float64(nsv))

					/* Convert to degrees. */
					mm.heading = int(heading * 360 / (math.Pi * 2))
					/* We don't want negative values but a 0-360 scale. */
					if mm.heading < 0 {
						mm.heading += 360
					}
				} else {
					mm.heading = 0
				}
			} else if mm.mesub == 3 || mm.mesub == 4 {
				mm.heading_is_valid = int(msg[5]) & (1 << 2)
				mm.heading = int((360.0 / 128) * float64(((int(msg[5])&3)<<5)|(int(msg[6])>>3)))
			}
		}
	}

	mm.phase_corrected = 0 /* Set to 1 by the caller if needed. */
}
