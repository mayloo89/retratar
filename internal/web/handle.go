package web

import (
	"strings"

	"github.com/mayloo89/retratar/internal/user"
)

// handleFromHost extracts the handle from a page hostname. It returns false
// unless host is exactly one label below pagesHost.
func handleFromHost(host, pagesHost string) (string, bool) {
	suffix := "." + pagesHost
	if !strings.HasSuffix(host, suffix) {
		return "", false
	}
	handle := strings.TrimSuffix(host, suffix)
	if strings.Contains(handle, ".") {
		return "", false // deeper nesting is not a page
	}
	if !user.ValidHandle(handle) {
		return "", false
	}
	return handle, true
}
