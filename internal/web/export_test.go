package web

import "time"

// HoldPhotoSlot takes the photo-processing slot, as a slow upload would, and
// returns the function that gives it back. Call it after Handler.
func (s *Server) HoldPhotoSlot() (release func()) {
	s.photoSlot <- struct{}{}
	return func() { <-s.photoSlot }
}

// SetPhotoSlotWait shortens how long an upload waits for the slot.
func (s *Server) SetPhotoSlotWait(d time.Duration) { s.photoSlotWait = d }
