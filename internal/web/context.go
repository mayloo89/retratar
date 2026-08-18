package web

import "context"

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
