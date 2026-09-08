package user

// maxHandleLen bounds a handle so it fits a DNS label and a page title.
const maxHandleLen = 30

// reservedHandles are never assignable to a user. Each one would otherwise
// resolve to a hostname that reads as official.
var reservedHandles = map[string]struct{}{
	"admin": {}, "api": {}, "app": {}, "blog": {}, "cdn": {}, "dev": {},
	"docs": {}, "ftp": {}, "help": {}, "img": {}, "mail": {}, "me": {},
	"news": {}, "root": {}, "security": {}, "smtp": {}, "staging": {},
	"static": {}, "status": {}, "support": {}, "system": {}, "www": {},
}

// ValidHandle reports whether s may be used as a handle, and therefore as a
// DNS label. The rules are deliberately narrower than DNS allows: lowercase
// ASCII letters, digits and single inner hyphens only.
//
// Unicode is excluded on purpose. Homograph handles let one user impersonate
// another's hostname, and a page URL is the entire identity in this product.
func ValidHandle(s string) bool {
	if len(s) == 0 || len(s) > maxHandleLen {
		return false
	}
	if s[0] == '-' || s[len(s)-1] == '-' {
		return false
	}
	if _, reserved := reservedHandles[s]; reserved {
		return false
	}
	var prevHyphen bool
	for i := range len(s) {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9':
			prevHyphen = false
		case c == '-':
			if prevHyphen {
				return false // no "--", which is how punycode is encoded
			}
			prevHyphen = true
		default:
			return false
		}
	}
	return true
}
