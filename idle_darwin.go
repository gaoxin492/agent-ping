package main

import (
	"os/exec"
	"regexp"
	"strconv"
)

var hidIdle = regexp.MustCompile(`"HIDIdleTime" = (\d+)`)

// idleSeconds: time since the last keyboard/mouse input.
func idleSeconds() (float64, bool) {
	out, err := exec.Command("ioreg", "-c", "IOHIDSystem", "-d", "4").Output()
	if err != nil {
		return 0, false
	}
	m := hidIdle.FindSubmatch(out)
	if m == nil {
		return 0, false
	}
	ns, err := strconv.ParseFloat(string(m[1]), 64)
	if err != nil {
		return 0, false
	}
	return ns / 1e9, true
}
