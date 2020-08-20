package mode_s

import (
	"fmt"
	"math"
	"sync"
	"time"
)

const MODES_AIRCRAFT_TTL = 60 /* TTL before being removed */

/* Structure used to describe an aircraft in iteractive mode. */
type Aircraft struct {
	Addr     uint32    /* ICAO address */
	HexAddr  string    /* Printable ICAO address */
	Flight   string    /* Flight number */
	Altitude int       /* Altitude */
	Speed    int       /* Velocity computed from EW and NS components. */
	Track    int       /* Angle of flight. */
	Seen     time.Time /* Time at which the last packet was received. */
	Messages int64     /* Number of Mode S messages received. */

	/* Encoded latitude and longitude as extracted by odd and even
	 * CPR encoded messages. */
	OddCprLat  int
	OddCprLon  int
	EvenCprLat int
	EvenCprLon int

	Latitude, Longitude     float64 /* Coordinated obtained from CPR encoded data. */
	OddCprTime, EvenCprTime int64
}

/* Return a new aircraft structure for the interactive mode linked list
 * of aircrafts. */
func NewAircraft(addr uint32) *Aircraft {
	return &Aircraft{
		Addr:    addr,
		HexAddr: fmt.Sprintf("%06X", addr),
		Seen:    time.Now(),
		// all other fields = 0
	}
}

func (ac *Aircraft) Clone() *Aircraft {
	clone := Aircraft{}
	//deepcopier.Copy(ac).To(clone)
	clone = *ac

	return &clone
}

type Sky struct {
	aircrafts    map[uint32]*Aircraft
	aircraft_ttl int /* TTL before deletion. */

	mux sync.Mutex
}

func NewSky() *Sky {
	return &Sky{
		aircrafts:    make(map[uint32]*Aircraft),
		aircraft_ttl: MODES_AIRCRAFT_TTL,
	}
}

// return copy of aircrafts data
func (sky *Sky) Aircrafts() map[uint32]*Aircraft {
	sky.mux.Lock()
	defer sky.mux.Unlock()

	clone := make(map[uint32]*Aircraft)
	for addr, ac := range sky.aircrafts {
		clone[addr] = ac.Clone()
	}

	return clone
}

func (sky *Sky) AircraftCount() int {
	sky.mux.Lock()
	defer sky.mux.Unlock()

	return len(sky.aircrafts)
}

func (sky *Sky) UpdateData(mm *ModeSMessage) *Aircraft {
	sky.mux.Lock()
	defer sky.mux.Unlock()

	// CRC check
	if !mm.crcok {
		return nil
	}

	var addr uint32
	addr = (mm.aa1 << 16) | (mm.aa2 << 8) | mm.aa3

	/* Loookup our aircraft or create a new one. */
	a := sky.aircrafts[addr]
	if a == nil {
		a = NewAircraft(addr)
		sky.aircrafts[addr] = a
	}

	a.Seen = time.Now()
	a.Messages++

	if mm.msgtype == 0 || mm.msgtype == 4 || mm.msgtype == 20 {
		a.Altitude = mm.altitude
	} else if mm.msgtype == 17 {
		if mm.metype >= 1 && mm.metype <= 4 {
			a.Flight = string(mm.flight[:])
		} else if mm.metype >= 9 && mm.metype <= 18 {
			a.Altitude = mm.altitude
			if mm.fflag != 0 {
				a.OddCprLat = mm.raw_latitude
				a.OddCprLon = mm.raw_longitude
				a.OddCprTime = mstime()
			} else {
				a.EvenCprLat = mm.raw_latitude
				a.EvenCprLon = mm.raw_longitude
				a.EvenCprTime = mstime()
			}
			/* If the two data is less than 10 seconds apart, compute
			 * the position. */
			if math.Abs(float64(a.EvenCprTime-a.OddCprTime)) <= 10000 {
				decodeCPR(a)
			}
		} else if mm.metype == 19 {
			if mm.mesub == 1 || mm.mesub == 2 {
				a.Speed = mm.velocity
				a.Track = mm.heading
			}
		}
	}

	return a
}

/* This algorithm comes from:
 * http://www.lll.lu/~edward/edward/adsb/DecodingADSBposition.html.
 *
 *
 * A few remarks:
 * 1) 131072 is 2^17 since CPR latitude and longitude are encoded in 17 bits.
 * 2) We assume that we always received the odd packet as last packet for
 *    simplicity. This may provide a position that is less fresh of a few
 *    seconds.
 */
func decodeCPR(a *Aircraft) {
	const AirDlat0 float64 = 360.0 / 60
	const AirDlat1 float64 = 360.0 / 59
	lat0 := float64(a.EvenCprLat)
	lat1 := float64(a.OddCprLat)
	lon0 := float64(a.EvenCprLon)
	lon1 := float64(a.OddCprLon)

	/* Compute the Latitude Index "j" */
	j := int(math.Floor(((59*lat0 - 60*lat1) / 131072) + 0.5))
	rlat0 := AirDlat0 * (float64(cprModFunction(j, 60)) + lat0/131072)
	rlat1 := AirDlat1 * (float64(cprModFunction(j, 59)) + lat1/131072)

	if rlat0 >= 270 {
		rlat0 -= 360
	}
	if rlat1 >= 270 {
		rlat1 -= 360
	}

	/* Check that both are in the same latitude zone, or abort. */
	if cprNLFunction(rlat0) != cprNLFunction(rlat1) {
		return
	}

	/* Compute ni and the longitude index m */
	if a.EvenCprTime > a.OddCprTime {
		/* Use even packet. */
		var ni int = cprNFunction(rlat0, 0)
		m := math.Floor((((lon0 * float64(cprNLFunction(rlat0)-1)) -
			(lon1 * float64(cprNLFunction(rlat0)))) / 131072) + 0.5)
		a.Longitude = cprDlonFunction(rlat0, 0) * (float64(cprModFunction(int(m), ni)) + lon0/131072)
		a.Latitude = rlat0
	} else {
		/* Use odd packet. */
		var ni int = cprNFunction(rlat1, 1)
		m := math.Floor((((lon0 * float64(cprNLFunction(rlat1)-1)) -
			(lon1 * float64(cprNLFunction(rlat1)))) / 131072.0) + 0.5)
		a.Longitude = cprDlonFunction(rlat1, 1) * (float64(cprModFunction(int(m), ni)) + lon1/131072)
		a.Latitude = rlat1
	}
	if a.Longitude > 180 {
		a.Longitude -= 360
	}
}

/* Always positive MOD operation, used for CPR decoding. */
func cprModFunction(a, b int) int {
	res := a % b
	if res < 0 {
		res += b
	}

	return res
}

/* The NL function uses the precomputed table from 1090-WP-9-14 */
func cprNLFunction(lat float64) int {
	/* Table is simmetric about the equator. */
	if lat < 0 {
		lat = -lat
	}

	switch {
	case lat < 10.47047130:
		return 59
	case lat < 14.82817437:
		return 58
	case lat < 18.18626357:
		return 57
	case lat < 21.02939493:
		return 56
	case lat < 23.54504487:
		return 55
	case lat < 25.82924707:
		return 54
	case lat < 27.93898710:
		return 53
	case lat < 29.91135686:
		return 52
	case lat < 31.77209708:
		return 51
	case lat < 33.53993436:
		return 50
	case lat < 35.22899598:
		return 49
	case lat < 36.85025108:
		return 48
	case lat < 38.41241892:
		return 47
	case lat < 39.92256684:
		return 46
	case lat < 41.38651832:
		return 45
	case lat < 42.80914012:
		return 44
	case lat < 44.19454951:
		return 43
	case lat < 45.54626723:
		return 42
	case lat < 46.86733252:
		return 41
	case lat < 48.16039128:
		return 40
	case lat < 49.42776439:
		return 39
	case lat < 50.67150166:
		return 38
	case lat < 51.89342469:
		return 37
	case lat < 53.09516153:
		return 36
	case lat < 54.27817472:
		return 35
	case lat < 55.44378444:
		return 34
	case lat < 56.59318756:
		return 33
	case lat < 57.72747354:
		return 32
	case lat < 58.84763776:
		return 31
	case lat < 59.95459277:
		return 30
	case lat < 61.04917774:
		return 29
	case lat < 62.13216659:
		return 28
	case lat < 63.20427479:
		return 27
	case lat < 64.26616523:
		return 26
	case lat < 65.31845310:
		return 25
	case lat < 66.36171008:
		return 24
	case lat < 67.39646774:
		return 23
	case lat < 68.42322022:
		return 22
	case lat < 69.44242631:
		return 21
	case lat < 70.45451075:
		return 20
	case lat < 71.45986473:
		return 19
	case lat < 72.45884545:
		return 18
	case lat < 73.45177442:
		return 17
	case lat < 74.43893416:
		return 16
	case lat < 75.42056257:
		return 15
	case lat < 76.39684391:
		return 14
	case lat < 77.36789461:
		return 13
	case lat < 78.33374083:
		return 12
	case lat < 79.29428225:
		return 11
	case lat < 80.24923213:
		return 10
	case lat < 81.19801349:
		return 9
	case lat < 82.13956981:
		return 8
	case lat < 83.07199445:
		return 7
	case lat < 83.99173563:
		return 6
	case lat < 84.89166191:
		return 5
	case lat < 85.75541621:
		return 4
	case lat < 86.53536998:
		return 3
	case lat < 87.00000000:
		return 2
	default:
		return 1
	}
}

func cprNFunction(lat float64, isodd int) int {
	nl := cprNLFunction(lat) - isodd
	if nl < 1 {
		nl = 1
	}
	return nl
}

func cprDlonFunction(lat float64, isodd int) float64 {
	return 360.0 / float64(cprNFunction(lat, isodd))
}

/* When in interactive mode If we don't receive new nessages within
 * MODES_AIRCRAFT_TTL seconds we remove the aircraft from the list. */
func (sky *Sky) RemoveStaleAircrafts() {
	sky.mux.Lock()
	defer sky.mux.Unlock()

	now := time.Now()

	remKeys := make([]uint32, 0)

	for k, a := range sky.aircrafts {
		dur := now.Sub(a.Seen)
		if int(dur.Seconds()) > sky.aircraft_ttl {
			remKeys = append(remKeys, k)
		}
	}

	for _, k := range remKeys {
		delete(sky.aircrafts, k)
	}
}
