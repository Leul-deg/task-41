package masking

import "regexp"

var ssnPattern = regexp.MustCompile(`\b\d{3}-?\d{2}-?\d{4}\b`)

func MaskSSN(input string) string {
	if !ssnPattern.MatchString(input) {
		return input
	}
	return ssnPattern.ReplaceAllStringFunc(input, func(s string) string {
		digits := []rune{}
		for _, r := range s {
			if r >= '0' && r <= '9' {
				digits = append(digits, r)
			}
		}
		if len(digits) < 4 {
			return "***-**-****"
		}
		return "***-**-" + string(digits[len(digits)-4:])
	})
}
