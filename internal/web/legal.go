package web

import "net/http"

// legalData is what privacy.html and terms.html render. Both values come from
// configuration, never from the repository.
type legalData struct {
	OperatorName string
	ContactEmail string
}

// handlePrivacy serves the privacy policy. It needs no session: a signed-in
// visitor sees the same page.
func (s *Server) handlePrivacy(w http.ResponseWriter, _ *http.Request) {
	s.renderTemplate(w, http.StatusOK, "privacy.html", s.legalData())
}

// handleTerms serves the terms of use, on the same terms as [Server.handlePrivacy].
func (s *Server) handleTerms(w http.ResponseWriter, _ *http.Request) {
	s.renderTemplate(w, http.StatusOK, "terms.html", s.legalData())
}

func (s *Server) legalData() legalData {
	return legalData{OperatorName: s.Config.OperatorName, ContactEmail: s.Config.ContactEmail}
}
