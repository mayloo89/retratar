package web

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"mime/multipart"
	"net/http"
	"strings"
	"time"

	"github.com/mayloo89/retratar/internal/photo"
	"github.com/mayloo89/retratar/internal/photostore"
	"github.com/mayloo89/retratar/internal/user"
)

// photoUploadPath is the one app route whose body is bigger than maxFormBody;
// see [BodyLimit].
const photoUploadPath = "/photo"

// maxPhotoBody is the most POST /photo reads: the photo itself plus 1 MiB of
// multipart framing and the other form fields.
const maxPhotoBody = photo.MaxBytes + 1<<20

// photoSlotWait is how long an upload waits for the processing slot before it
// is told to try again.
const photoSlotWait = 5 * time.Second

const (
	msgPhotoNone       = "Elegí una foto."
	msgPhotoToday      = "Ya cambiaste tu foto hoy. Mañana podés subir otra."
	msgPhotoBusy       = "Estamos procesando otra foto. Probá de nuevo en unos segundos."
	msgPhotoTooLarge   = "Esta foto es muy grande. Probá con una sacada en modo normal o más chica."
	msgPhotoUnreadable = "No pudimos leer esa foto. Probá con un JPG o PNG."
)

// now is the current time. Tests replace it through Server.Now.
func (s *Server) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// dayStart is midnight of t's calendar day in Argentina. It is passed to the
// database as an absolute instant, so the day boundary never depends on the
// database server's time zone.
func dayStart(t time.Time) time.Time {
	y, m, d := t.In(argentina).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, argentina)
}

// photoURL is the app-host path of a photo, for the dashboard preview.
func photoURL(key string) string { return "/photo/" + key + ".jpg" }

// handlePhotoUpload stores a new photo for an allowlisted account.
//
// The order matters: the cheap checks come first, the body is read before the
// processing slot is taken, the new file is written before the database
// commit, and the old file is deleted only after it.
func (s *Server) handlePhotoUpload(w http.ResponseWriter, r *http.Request) {
	u, ok := UserFrom(r.Context())
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	if u.State != user.StateActive {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	// The form is only shown to allowlisted accounts, so this is a
	// hand-made request.
	if !u.CanUploadPhotos {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	now := s.now()
	// A cheap early exit. SetPhoto enforces the rule for real, below.
	if u.UploadedPhotoOn(now.In(argentina)) {
		s.renderAppHomeMessages(w, r, http.StatusUnprocessableEntity, u, homeMessages{Photo: msgPhotoToday})
		return
	}

	// This route is exempt from BodyLimit (see there), so it sets its own cap.
	if r.ContentLength > maxPhotoBody {
		http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxPhotoBody)

	data, err := readPhotoPart(r)
	if err != nil {
		var tooBig *http.MaxBytesError
		switch {
		case errors.As(err, &tooBig):
			http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
		case errors.Is(err, errNoPhotoPart):
			s.renderAppHomeMessages(w, r, http.StatusUnprocessableEntity, u, homeMessages{Photo: msgPhotoNone})
		default:
			http.Error(w, "bad request", http.StatusBadRequest)
		}
		return
	}

	res, status, msg := s.processPhoto(r, data)
	if status != 0 {
		if msg == "" {
			http.Error(w, "internal server error", status)
			return
		}
		s.renderAppHomeMessages(w, r, status, u, homeMessages{Photo: msg})
		return
	}

	key := photostore.NewKey()
	if err = s.Photos.Save(key, res.JPEG); err != nil {
		s.Logger.ErrorContext(r.Context(), "save photo", slog.String("error", err.Error()))
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	oldKey, err := s.Users.SetPhoto(r.Context(), u.ID, key, res.Width, res.Height, dayStart(now), now)
	if err != nil {
		s.removePhotoFile(r, key)
		if errors.Is(err, user.ErrPhotoNotAllowed) {
			// Two uploads passed the early check together; this one lost.
			s.renderAppHomeMessages(w, r, http.StatusUnprocessableEntity, u, homeMessages{Photo: msgPhotoToday})
			return
		}
		s.Logger.ErrorContext(r.Context(), "set photo", slog.String("error", err.Error()))
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	if oldKey != "" {
		s.removePhotoFile(r, oldKey)
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// processPhoto runs photo.Process one upload at a time: decoding a photo can
// take up to MaxDecodeBytes, so two at once on a small machine is too many.
// It returns a non-zero status, and the message to show with it, on failure; a
// zero status means success. An empty message with a status is a plain error
// response.
func (s *Server) processPhoto(r *http.Request, data []byte) (photo.Result, int, string) {
	timer := time.NewTimer(s.photoWait())
	defer timer.Stop()

	select {
	case s.photoSlot <- struct{}{}:
	case <-timer.C:
		return photo.Result{}, http.StatusServiceUnavailable, msgPhotoBusy
	case <-r.Context().Done():
		return photo.Result{}, http.StatusServiceUnavailable, ""
	}
	res, err := s.processLocked(data)

	switch {
	case err == nil:
		return res, 0, ""
	case errors.Is(err, photo.ErrTooLarge):
		return photo.Result{}, http.StatusUnprocessableEntity, msgPhotoTooLarge
	case errors.Is(err, photo.ErrUnsupportedFormat), errors.Is(err, photo.ErrInvalidImage):
		return photo.Result{}, http.StatusUnprocessableEntity, msgPhotoUnreadable
	default:
		s.Logger.ErrorContext(r.Context(), "process photo", slog.String("error", err.Error()))
		return photo.Result{}, http.StatusInternalServerError, ""
	}
}

// processLocked runs the processor while the caller holds the slot, and gives
// the slot back on every exit, a panic included. Net/http recovers a panic for
// its one request; a slot left full would turn every later upload into a 503
// until the process restarted.
func (s *Server) processLocked(data []byte) (photo.Result, error) {
	defer func() { <-s.photoSlot }()
	process := s.process
	if process == nil {
		process = photo.Process
	}
	return process(bytes.NewReader(data))
}

func (s *Server) photoWait() time.Duration {
	if s.photoSlotWait > 0 {
		return s.photoSlotWait
	}
	return photoSlotWait
}

// errNoPhotoPart means the request had no usable "photo" part.
var errNoPhotoPart = errors.New("no photo part")

// readPhotoPart returns the bytes of the multipart part named "photo".
//
// It streams with MultipartReader on purpose. ParseMultipartForm spills large
// files to temporary files on disk, and the container's filesystem is
// read-only. At most MaxBytes+1 bytes are kept, which photo.Process reports as
// ErrTooLarge.
func readPhotoPart(r *http.Request) ([]byte, error) {
	mr, err := r.MultipartReader()
	if err != nil {
		if errors.Is(err, http.ErrNotMultipart) || errors.Is(err, http.ErrMissingBoundary) {
			return nil, errNoPhotoPart
		}
		return nil, err
	}
	for {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			return nil, errNoPhotoPart
		}
		if err != nil {
			return nil, err
		}
		data, found, err := photoPartBytes(part)
		if err != nil {
			return nil, err
		}
		if found {
			if len(data) == 0 {
				return nil, errNoPhotoPart
			}
			return data, nil
		}
	}
}

func photoPartBytes(part *multipart.Part) ([]byte, bool, error) {
	defer part.Close() //nolint:errcheck // closing a multipart part cannot fail
	if part.FormName() != "photo" {
		return nil, false, nil
	}
	data, err := io.ReadAll(io.LimitReader(part, photo.MaxBytes+1))
	if err != nil {
		return nil, true, err
	}
	return data, true, nil
}

// handlePhotoRemove removes the signed-in account's photo. There is no
// allowlist check: anyone can remove their own photo, even after losing the
// right to upload one.
func (s *Server) handlePhotoRemove(w http.ResponseWriter, r *http.Request) {
	u, ok := UserFrom(r.Context())
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	if u.State != user.StateActive {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	oldKey, err := s.Users.RemovePhoto(r.Context(), u.ID)
	if err != nil {
		s.Logger.ErrorContext(r.Context(), "remove photo", slog.String("error", err.Error()))
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if oldKey != "" {
		s.removePhotoFile(r, oldKey)
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// removePhotoFile deletes a stored photo and logs, but does not fail the
// request, when it cannot: a file left behind is harmless, an error page for
// work that already succeeded is not.
func (s *Server) removePhotoFile(r *http.Request, key string) {
	if err := s.Photos.Remove(key); err != nil {
		s.Logger.ErrorContext(r.Context(), "remove photo file",
			slog.String("key", key), slog.String("error", err.Error()))
	}
}

// keyFromFile extracts the photo key from a "<key>.jpg" path segment.
func keyFromFile(file string) (string, bool) {
	key, ok := strings.CutSuffix(file, ".jpg")
	if !ok || !photostore.ValidKey(key) {
		return "", false
	}
	return key, true
}

// handlePagePhoto serves a photo on its owner's page host, publicly. A photo
// lives only under the page of the account that holds it.
func (s *Server) handlePagePhoto(w http.ResponseWriter, r *http.Request) {
	key, ok := keyFromFile(r.PathValue("file"))
	handle, hasHandle := HandleFrom(r.Context())
	if !ok || !hasHandle {
		http.NotFound(w, r)
		return
	}

	u, err := s.Users.ByPhotoKey(r.Context(), key)
	if err != nil {
		if errors.Is(err, user.ErrUserNotFound) {
			http.NotFound(w, r)
			return
		}
		s.Logger.ErrorContext(r.Context(), "get user by photo key", slog.String("error", err.Error()))
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if !strings.EqualFold(u.Handle, handle) {
		http.NotFound(w, r)
		return
	}

	// The key changes with every upload, so the URL names these exact bytes.
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	s.servePhotoFile(w, r, key)
}

// handleAppPhoto serves a photo on the app host, to its signed-in owner only,
// for the dashboard preview. A page host cannot serve the dashboard's <img>:
// it is a different site and Cross-Origin-Resource-Policy blocks it. The app
// host stays private (no-store, from PrivateCache) and is not an image host.
func (s *Server) handleAppPhoto(w http.ResponseWriter, r *http.Request) {
	key, ok := keyFromFile(r.PathValue("file"))
	u, signedIn := UserFrom(r.Context())
	if !ok || !signedIn || u.PhotoKey != key {
		http.NotFound(w, r)
		return
	}
	s.servePhotoFile(w, r, key)
}

// servePhotoFile writes the stored JPEG for key. A key the database names but
// the disk does not have is a 404 and an error in the log: the data is gone,
// which is not the visitor's mistake.
func (s *Server) servePhotoFile(w http.ResponseWriter, r *http.Request, key string) {
	f, err := s.Photos.Open(key)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			s.Logger.ErrorContext(r.Context(), "photo file missing", slog.String("key", key))
		} else {
			s.Logger.ErrorContext(r.Context(), "open photo", slog.String("key", key), slog.String("error", err.Error()))
		}
		http.NotFound(w, r)
		return
	}
	defer f.Close() //nolint:errcheck // read-only file

	info, err := f.Stat()
	if err != nil {
		s.Logger.ErrorContext(r.Context(), "stat photo", slog.String("key", key), slog.String("error", err.Error()))
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	http.ServeContent(w, r, "", info.ModTime(), f)
}
