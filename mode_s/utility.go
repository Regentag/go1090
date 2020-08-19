package mode_s

import "time"

// See: https://stackoverflow.com/a/24122933
func mstime() int64 {
	return time.Now().UnixNano() / int64(time.Millisecond)
}
