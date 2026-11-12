package textutil

import "strings"

// FormatFullName joins the parts of a name with single spaces.
func FormatFullName(first, last string) string {
	return strings.TrimSpace(first) + " " + strings.TrimSpace(last)
}

// Initials returns the first letters, upper case.
func Initials(first, last string) string {
	return strings.ToUpper(first[:1] + last[:1])
}
