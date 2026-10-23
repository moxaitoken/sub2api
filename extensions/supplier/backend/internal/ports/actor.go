package ports

import "context"

type adminActorKey struct{}

// WithAdminActor is set only after main administrator authorization, never from input.
func WithAdminActor(ctx context.Context, id int64) context.Context {
	return context.WithValue(ctx, adminActorKey{}, id)
}
func AdminActor(ctx context.Context) int64 { id, _ := ctx.Value(adminActorKey{}).(int64); return id }
