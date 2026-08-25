package web

import (
	"context"

	"github.com/mayloo89/retratar/internal/user"
)

const handleKey contextKey = "handle"

// withHandle stores the page handle resolved from the request's hostname.
func withHandle(ctx context.Context, handle string) context.Context {
	return context.WithValue(ctx, handleKey, handle)
}

// HandleFrom returns the page handle for the current request. It reports false
// on the app surface, where no handle exists.
//
// Page handlers must read the handle from here rather than re-parsing r.Host.
// The hostname is parsed and validated exactly once, at the routing boundary.
func HandleFrom(ctx context.Context) (string, bool) {
	handle, ok := ctx.Value(handleKey).(string)
	return handle, ok
}

const userKey contextKey = "user"

// withUser stores the account resolved from the request's session cookie.
func withUser(ctx context.Context, u user.User) context.Context {
	return context.WithValue(ctx, userKey, u)
}

// UserFrom returns the account making the current request. It reports false
// when the request carries no valid session — that is not an error on its
// own; whether a route requires a session is up to the handler.
func UserFrom(ctx context.Context) (user.User, bool) {
	u, ok := ctx.Value(userKey).(user.User)
	return u, ok
}
