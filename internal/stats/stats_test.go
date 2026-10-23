package stats_test

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mayloo89/retratar/internal/stats"
	"github.com/mayloo89/retratar/internal/store"
	"github.com/mayloo89/retratar/internal/testdb"
)

var art = time.FixedZone("ART", -3*60*60)

// now is the fixed clock every number is measured from.
var now = time.Date(2026, 10, 30, 21, 0, 0, 0, art)

func ago(d time.Duration) time.Time { return now.Add(-d) }

const day = 24 * time.Hour

type seeder struct {
	t    *testing.T
	pool *pgxpool.Pool
	n    int
}

// user inserts an account created age before now. A handle makes it a page.
func (s *seeder) user(age time.Duration, withHandle bool, extra string) uuid.UUID {
	s.t.Helper()
	s.n++
	id := uuid.New()
	var handle any
	if withHandle {
		handle = "h" + strings.Repeat("x", s.n)
	}
	s.exec(`INSERT INTO users (id, email, handle, created_at) VALUES ($1, $2, $3, $4)`,
		id, "u"+strings.Repeat("x", s.n)+"@example.com", handle, ago(age))
	if extra != "" {
		s.exec(`UPDATE users SET `+extra+` WHERE id = $1`, id)
	}
	return id
}

func (s *seeder) session(u uuid.UUID, at time.Time) {
	s.t.Helper()
	s.n++
	s.exec(`INSERT INTO sessions (session_hash, user_id, created_at, expires_at) VALUES ($1, $2, $3, $4)`,
		[]byte{byte(s.n), byte(s.n >> 8)}, u, at, at.Add(30*day))
}

func (s *seeder) mood(u uuid.UUID, at time.Time) {
	s.t.Helper()
	s.exec(`INSERT INTO mood_history (user_id, mood_key, at) VALUES ($1, 'feliz', $2)`, u, at)
}

func (s *seeder) sign(page, author uuid.UUID, state string, at time.Time) {
	s.t.Helper()
	s.exec(`INSERT INTO guestbook_entries (page_user_id, author_user_id, body, state, created_at) VALUES ($1, $2, 'hola', $3, $4)`,
		page, author, state, at)
}

func (s *seeder) exec(sql string, args ...any) {
	s.t.Helper()
	if _, err := s.pool.Exec(s.t.Context(), sql, args...); err != nil {
		s.t.Fatalf("seed %q: %v", sql, err)
	}
}

func TestCollect(t *testing.T) {
	t.Parallel()
	pool := testdb.New(t)
	s := &seeder{t: t, pool: pool}

	// Week-two cohort: every account here is a page at least 14 days old
	// except "young".
	pass := s.user(30*day, true, "") // 2 changes inside [7, 14): passes
	s.mood(pass, ago(30*day).Add(8*day))
	s.mood(pass, ago(30*day).Add(10*day))

	early := s.user(30*day, true, "") // 2 changes, one at day 6: fails
	s.mood(early, ago(30*day).Add(6*day))
	s.mood(early, ago(30*day).Add(9*day))

	edge := s.user(30*day, true, "") // one in the window, one at day 14 exactly: excluded
	s.mood(edge, ago(30*day).Add(9*day))
	s.mood(edge, ago(30*day).Add(14*day))

	young := s.user(13*day, true, "") // 13 days old: not eligible
	s.mood(young, ago(13*day).Add(8*day))
	s.mood(young, ago(13*day).Add(9*day))

	exact := s.user(14*day, true, "") // exactly 14 days old: eligible, no changes
	unclaimed := s.user(40*day, false, "")

	// Sessions: pass (twice, one user), early (6d ago), exact (8d ago: outside).
	s.session(pass, ago(1*day))
	s.session(pass, ago(2*day))
	s.session(early, ago(6*day))
	s.session(exact, ago(8*day))

	// Recent mood changes: pass and early are 30 days old so their rows above
	// are old; add recent ones.
	s.mood(pass, ago(1*day))
	s.mood(pass, ago(3*day))
	s.mood(early, ago(2*day))
	s.mood(unclaimed, ago(8*day)) // outside 7d

	// Guestbook: pass<->early reciprocal (two entries one way still one pair);
	// pass->edge one-way; edge<->young where one side is hidden (still a pair).
	s.sign(pass, early, "visible", ago(1*day))
	s.sign(pass, early, "visible", ago(2*day))
	s.sign(early, pass, "visible", ago(20*day))
	s.sign(edge, pass, "visible", ago(3*day))
	s.sign(edge, young, "visible", ago(9*day))
	s.sign(young, edge, "hidden", ago(10*day))

	// Photos: pass has one uploaded 2 days ago; edge uploaded 10 days ago;
	// early removed theirs (key cleared, upload time kept, 3 days ago).
	s.exec(`UPDATE users SET photo_key = $2, photo_width = 10, photo_height = 10, photo_uploaded_at = $3, can_upload_photos = true WHERE id = $1`,
		pass, "aaaaaaaaaaaaaaaaaaaaaaaaaa", ago(2*day))
	s.exec(`UPDATE users SET photo_key = $2, photo_width = 10, photo_height = 10, photo_uploaded_at = $3 WHERE id = $1`,
		edge, "bbbbbbbbbbbbbbbbbbbbbbbbbb", ago(10*day))
	s.exec(`UPDATE users SET photo_uploaded_at = $2, can_upload_photos = true WHERE id = $1`, early, ago(3*day))

	// Rows stamped an hour after now are outside every 7d window. Each is
	// attached so the other expected numbers do not move; only the unbounded
	// guestbook total counts the entry.
	future := now.Add(time.Hour)
	s.session(exact, future)
	s.mood(unclaimed, future)
	s.sign(pass, early, "visible", future)
	s.exec(`UPDATE users SET photo_uploaded_at = $2 WHERE id = $1`, exact, future)

	got, err := stats.Collect(t.Context(), store.New(pool), now.UTC())
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}

	if !got.At.Equal(now) || got.At.Location().String() != "ART" {
		t.Errorf("At = %v, want %v in ART", got.At, now)
	}
	got.At = time.Time{}

	want := stats.Report{
		Accounts:        6,
		Pages:           5,
		SignedIn:        2, // pass and early; exact's session is 8 days old
		MoodChanges:     5, // pass 2, early 1, young 2
		ChangedMood:     3,
		WeekTwoEligible: 4, // pass, early, edge, exact (14 days exactly counts)
		WeekTwoPassed:   1, // only pass
		GuestbookTotal:  7, // includes the entry stamped after now
		Guestbook7d:     3,
		ReciprocalPairs: 2, // {pass, early} once despite two entries; {edge, young} with one hidden
		PhotosNow:       2,
		PhotoUploads7d:  2, // pass and early; edge's is 10 days old
		Allowlisted:     2,
	}
	if got != want {
		t.Errorf("Collect() =\n%+v\nwant\n%+v", got, want)
	}
}

func TestReport_WriteTo(t *testing.T) {
	t.Parallel()

	r := stats.Report{
		At:              time.Date(2026, 10, 30, 21, 0, 0, 0, art),
		Accounts:        12,
		Pages:           10,
		SignedIn:        8,
		MoodChanges:     23,
		ChangedMood:     7,
		WeekTwoEligible: 6,
		WeekTwoPassed:   3,
		GuestbookTotal:  31,
		Guestbook7d:     12,
		ReciprocalPairs: 5,
		PhotosNow:       4,
		PhotoUploads7d:  6,
		Allowlisted:     5,
	}
	want := `retratar stats at 2026-10-30 21:00 ART

accounts           12
pages (handle)     10
signed in, 7d       8      distinct users with a session created in the last 7 days

mood changes, 7d   23      rows in mood_history in the last 7 days
changed mood, 7d    7      distinct users with at least one change in the last 7 days
week two
  eligible          6      pages whose account is at least 14 days old
  2+ changes        3      of those, how many changed mood 2+ times between day 7 and day 14 of their account

guestbook, total   31
guestbook, 7d      12
reciprocal pairs    5      unordered pairs {a, b} where a signed b's book and b signed a's (any state)

photos now          4      accounts with a current photo
photo uploads, 7d   6      photo_uploaded_at in the last 7 days (one per account at most; it's the last upload)
allowlisted         5
`
	var buf bytes.Buffer
	n, err := r.WriteTo(&buf)
	if err != nil {
		t.Fatalf("WriteTo() error = %v", err)
	}
	if n != int64(buf.Len()) {
		t.Errorf("WriteTo() n = %d, want %d", n, buf.Len())
	}
	if buf.String() != want {
		t.Errorf("WriteTo() =\n%s\nwant\n%s", buf.String(), want)
	}
}
