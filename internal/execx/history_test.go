package execx

import (
	"context"
	"errors"
	"testing"
)

func TestHistoryRunner(t *testing.T) {
	f := NewFake()
	f.Set([]string{"sinfo", "--version"}, FakeResponse{Stdout: []byte("slurm 23.11.4\n")})
	h := WithHistory(f, 2, nil)
	ctx := context.Background()
	if _, err := h.Run(ctx, "sinfo", "--version"); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Run(ctx, "scancel", "1"); !errors.Is(err, ErrMutationNotAuthorized) {
		t.Fatalf("scancel without grant: %v", err)
	}
	if got := h.History(); len(got) != 2 || got[0].Err != "" || !got[1].Refused {
		t.Fatalf("history = %+v", got)
	}
	if _, err := h.Run(ctx, "sinfo", "--version"); err != nil {
		t.Fatal(err)
	}
	if got := h.History(); len(got) != 2 || !got[0].Refused || Key(got[1].Argv) != "sinfo --version" {
		t.Fatalf("ring = %+v", got)
	}
}
