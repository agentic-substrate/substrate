package strictjson

import (
	"strconv"
	"unicode/utf8"
)

func ValidText(line []byte) bool { return utf8.Valid(line) && validSurrogates(line) }

func validSurrogates(line []byte) bool {
	for i := 0; i < len(line); i++ {
		if line[i] != '\\' {
			continue
		}
		i++
		if i >= len(line) {
			return false
		}
		if line[i] != 'u' {
			continue
		}
		if i+4 >= len(line) {
			return false
		}
		value, err := strconv.ParseUint(string(line[i+1:i+5]), 16, 16)
		if err != nil {
			return false
		}
		i += 4
		if value >= 0xDC00 && value <= 0xDFFF {
			return false
		}
		if value >= 0xD800 && value <= 0xDBFF {
			if i+6 >= len(line) || line[i+1] != '\\' || line[i+2] != 'u' {
				return false
			}
			low, err := strconv.ParseUint(string(line[i+3:i+7]), 16, 16)
			if err != nil || low < 0xDC00 || low > 0xDFFF {
				return false
			}
			i += 6
		}
	}
	return true
}
