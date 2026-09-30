package execx

import (
	"context"
	"errors"
	"slices"
	"testing"
)

func TestNewInteractiveChecked(t *testing.T) {
	ctx := context.Background()
	argv := []string{"srun", "--jobid=812", "--overlap", "--pty", "bash", "-l"}
	if _, err := NewInteractiveChecked(ctx, Policy{}, "", argv...); !errors.Is(err, ErrClusterToolInTests) {
		t.Fatalf("test guard: %v", err)
	}
	p := Policy{NoTestGuard: true}
	if _, err := NewInteractiveChecked(ctx, p, "", argv...); !errors.Is(err, ErrMutationNotAuthorized) {
		t.Fatalf("no grant: %v", err)
	}
	other := append(slices.Clone(argv[:len(argv)-1]), "-i")
	if _, err := NewInteractiveChecked(WithMutation(ctx, "shell", other), p, "", argv...); !errors.Is(err, ErrMutationNotAuthorized) {
		t.Fatalf("grant for other argv: %v", err)
	}
	c, err := NewInteractiveChecked(WithMutation(ctx, "shell", argv), p, "", argv...)
	if err != nil || !slices.Equal(c.Argv(), argv) {
		t.Fatalf("granted: %v", err)
	}
	if _, err := NewInteractiveChecked(ctx, p, "", "vim"); !errors.Is(err, ErrNotAllowed) {
		t.Fatalf("non-cluster tool: %v", err)
	}
}
