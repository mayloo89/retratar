// Package stats prints the operator's numbers: aggregates read straight from
// the database, with no emails, handles, notes or entry text in them, so the
// output is safe to paste into a chat.
package stats

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/mayloo89/retratar/internal/store"
)

// argentina is the zone the report heading is written in. It is the same fixed
// zone as in internal/web/page.go (UTC-3 all year, no tzdata needed in the
// distroless image); keep the two in step.
var argentina = time.FixedZone("ART", -3*60*60)

// Report is one reading of the numbers. "7d" fields cover the 7 days up to At.
type Report struct {
	// At is when the report was taken, in Argentina time.
	At time.Time

	Accounts int64
	Pages    int64
	SignedIn int64

	MoodChanges int64
	ChangedMood int64

	WeekTwoEligible int64
	WeekTwoPassed   int64

	GuestbookTotal  int64
	Guestbook7d     int64
	ReciprocalPairs int64

	PhotosNow      int64
	PhotoUploads7d int64
	Allowlisted    int64
}

// Collect reads every number as of now. The queries take now as a parameter
// rather than calling now() themselves, which is what makes them testable.
func Collect(ctx context.Context, q *store.Queries, now time.Time) (Report, error) {
	row, err := q.CollectStats(ctx, pgtype.Timestamptz{Time: now, Valid: true})
	if err != nil {
		return Report{}, fmt.Errorf("collect stats: %w", err)
	}
	return Report{
		At:              now.In(argentina),
		Accounts:        row.Accounts,
		Pages:           row.Pages,
		SignedIn:        row.SignedIn7d,
		MoodChanges:     row.MoodChanges7d,
		ChangedMood:     row.ChangedMood7d,
		WeekTwoEligible: row.WeekTwoEligible,
		WeekTwoPassed:   row.WeekTwoPassed,
		GuestbookTotal:  row.GuestbookTotal,
		Guestbook7d:     row.Guestbook7d,
		ReciprocalPairs: row.ReciprocalPairs,
		PhotosNow:       row.PhotosNow,
		PhotoUploads7d:  row.PhotoUploads7d,
		Allowlisted:     row.Allowlisted,
	}, nil
}

// WriteTo writes the plain-text report. The note after a number is printed as
// is, so the operator knows exactly what it counts.
func (r Report) WriteTo(w io.Writer) (int64, error) {
	var b strings.Builder
	line := func(label string, n int64, note string) {
		fmt.Fprintf(&b, "%-17s %3d", label, n)
		if note != "" {
			b.WriteString("      " + note)
		}
		b.WriteByte('\n')
	}

	fmt.Fprintf(&b, "retratar stats at %s\n\n", r.At.In(argentina).Format("2006-01-02 15:04 MST"))

	line("accounts", r.Accounts, "")
	line("pages (handle)", r.Pages, "")
	line("signed in, 7d", r.SignedIn, "distinct users with a session created in the last 7 days")
	b.WriteByte('\n')

	line("mood changes, 7d", r.MoodChanges, "rows in mood_history in the last 7 days")
	line("changed mood, 7d", r.ChangedMood, "distinct users with at least one change in the last 7 days")
	b.WriteString("week two\n")
	line("  eligible", r.WeekTwoEligible, "pages whose account is at least 14 days old")
	line("  2+ changes", r.WeekTwoPassed, "of those, how many changed mood 2+ times between day 7 and day 14 of their account")
	b.WriteByte('\n')

	line("guestbook, total", r.GuestbookTotal, "")
	line("guestbook, 7d", r.Guestbook7d, "")
	line("reciprocal pairs", r.ReciprocalPairs, "unordered pairs {a, b} where a signed b's book and b signed a's (any state)")
	b.WriteByte('\n')

	line("photos now", r.PhotosNow, "accounts with a current photo")
	line("photo uploads, 7d", r.PhotoUploads7d, "photo_uploaded_at in the last 7 days (one per account at most; it's the last upload)")
	line("allowlisted", r.Allowlisted, "")

	n, err := io.WriteString(w, b.String())
	return int64(n), err
}
