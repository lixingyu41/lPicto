package util

import "time"

func UnixNow() int64 {
	return time.Now().Unix()
}

func UnixNowNano() int64 {
	return time.Now().UnixNano()
}
