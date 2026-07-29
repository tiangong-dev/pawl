package pawl

import "os"

func round2(v float64) float64 {
	scaled := v * 100
	if scaled >= 0 {
		scaled = float64(int64(scaled + 0.5))
	} else {
		scaled = float64(int64(scaled - 0.5))
	}
	return scaled / 100
}

func onCI() bool {
	return os.Getenv("GITHUB_ACTIONS") != ""
}
