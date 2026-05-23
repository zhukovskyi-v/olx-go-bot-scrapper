package telegram

import (
	"net/url"
	"regexp"
)

var urlPattern = regexp.MustCompile(`^(https?://)?(www\.)?olx\.(ua|pl|bg|ro|pt|com|co\.za|com\.br|com\.pk|lt|lv|hr|kz|uz|by|md|az)/.*$`)

func isValidURL(str string) bool {
	if !urlPattern.MatchString(str) {
		return false
	}
	_, err := url.ParseRequestURI(str)
	return err == nil
}
