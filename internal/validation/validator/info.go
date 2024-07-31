package validator

import "strconv"

func GetInfoStringFloat64(prefix string, value float64, unit string) string {
	return prefix + " " + strconv.FormatFloat(value, 'f', 2, 64) + " " + unit
}

func GetInfoStringInt(prefix string, value int, unit string) string {
	return prefix + " " + strconv.Itoa(value) + " " + unit
}

func GetInfoStrString(prefix string, value string) string {
	return prefix + " " + value
}
