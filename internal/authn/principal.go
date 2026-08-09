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

func WithPrincipal(ctx context.Context, principal Principal) context.Context {
	return context.WithValue(ctx, principalContextKey{}, principal)
}

func PrincipalFromContext(ctx context.Context) (Principal, bool) {
	principal, ok := ctx.Value(principalContextKey{}).(Principal)
	return principal, ok
}
