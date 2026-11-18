package main

import (
	"fmt"
	"math"
)

func discordPageLimit(raw float64, maxLimit int) (int, error) {
	if math.Trunc(raw) != raw || raw < 1 || raw > float64(maxLimit) {
		return 0, fmt.Errorf("limit must be an integer from 1 to %d", maxLimit)
	}
	return int(raw), nil
}
