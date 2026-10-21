//go:build !darwin && !windows

package main

import (
	"os/exec"
	"strconv"
	"strings"
)

// idleSeconds uses xprintidle when it's installed; otherwise "unknown".
func idleSeconds() (float64, bool) {
	out, err := exec.Command("xprintidle").Output()
	if err != nil {
		return 0, false
	}
	ms, err := strconv.ParseFloat(strings.TrimSpace(string(out)), 64)
	if err != nil {
		return 0, false
	}
	return ms / 1000, true
}
