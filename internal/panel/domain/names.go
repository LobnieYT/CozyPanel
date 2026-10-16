package domain

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// DeviceNameMax: an own name longer than this does not fit the rows that show it.
const DeviceNameMax = 40

// CleanDeviceName trims an admin's or subscriber's own device name. Empty goes back
// to the name the app reports.
func CleanDeviceName(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", nil
	}
	if !utf8.ValidString(s) || !printableLine(s) {
		return "", fieldErr("name", "name_chars")
	}
	if len([]rune(s)) > DeviceNameMax {
		return "", fieldErr("name", "device_name_long")
	}
	return s, nil
}

// printableLine is one visible line: no control characters or invisible marks.
func printableLine(s string) bool {
	for _, r := range s {
		if unicode.Is(unicode.C, r) {
			return false
		}
	}
	return true
}
