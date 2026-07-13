package querycode

import "errors"

var ErrInvalid = errors.New("invalid query code")

func Validate(value string) error {
	if len(value) < 6 || len(value) > 32 {
		return ErrInvalid
	}
	hasLetterOrDigit := false
	for _, char := range value {
		switch {
		case char >= 'a' && char <= 'z':
			hasLetterOrDigit = true
		case char >= 'A' && char <= 'Z':
			hasLetterOrDigit = true
		case char >= '0' && char <= '9':
			hasLetterOrDigit = true
		case char == '-' || char == '_' || char == '@' || char == '#' || char == '.':
		default:
			return ErrInvalid
		}
	}
	if !hasLetterOrDigit {
		return ErrInvalid
	}
	return nil
}
