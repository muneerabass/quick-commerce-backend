package services

import (
	"errors"
	"regexp"
	"strings"
)

var (
	ErrInvalidPhone = errors.New("phone must be a valid E.164 number")
	phonePattern    = regexp.MustCompile(`^\+[1-9][0-9]{7,14}$`)
)

func NormalizePhone(value string) (string, error) {
	phone := strings.TrimSpace(value)
	phone = strings.NewReplacer(" ", "", "-", "", "(", "", ")", "").Replace(phone)
	if !phonePattern.MatchString(phone) {
		return "", ErrInvalidPhone
	}
	return phone, nil
}
