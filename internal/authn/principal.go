package authn

import "context"

type Principal struct {
	UserID         string
	ProfileID      string
	Username       string
	DisplayName    string
	Role           string
	SessionID      string
	SessionVersion int64
	SessionToken   string
}

type principalContextKey struct{}

type Actor struct {
	Kind   string
	UserID any
	Label  any
}

func WithPrincipal(ctx context.Context, principal Principal) context.Context {
	return context.WithValue(ctx, principalContextKey{}, principal)
}

func PrincipalFromContext(ctx context.Context) (Principal, bool) {
	principal, ok := ctx.Value(principalContextKey{}).(Principal)
	return principal, ok
}

// ActorFromContext projects an authenticated user when one exists. The
// fallback label is restricted by the database actor contract and is used by
// startup/recovery work that legitimately has no user session.
func ActorFromContext(ctx context.Context, fallbackLabel string) Actor {
	if principal, ok := PrincipalFromContext(ctx); ok && principal.UserID != "" {
		return Actor{Kind: "USER", UserID: principal.UserID, Label: nil}
	}
	return Actor{Kind: "SYSTEM", UserID: nil, Label: fallbackLabel}
}
