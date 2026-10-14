package config

import (
	"time"
	_ "time/tzdata" // embed timezone data in the binary
)

var Jakarta = mustLoad("Asia/Jakarta")

func mustLoad(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		panic(err)
	}
	return loc
}
