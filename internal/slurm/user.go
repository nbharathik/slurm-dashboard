package slurm

import (
	"context"
	"errors"
	"strings"

	"github.com/nbharathik/slurm-dashboard/internal/execx"
)

// User returns the current user name: $USER, else "id -un". It never uses
// Go's os/user, which cannot see LDAP/SSSD accounts in a static binary.
func User(ctx context.Context, getenv func(string) string, r execx.Runner) (string, error) {
	if u := strings.TrimSpace(getenv("USER")); u != "" {
		return u, nil
	}
	res, err := r.Run(execx.WithLabel(ctx, "user"), "id", "-un")
	if err != nil {
		return "", err
	}
	if u := strings.TrimSpace(string(res.Stdout)); u != "" {
		return u, nil
	}
	return "", errors.New("cannot determine the user name: $USER is empty and id -un printed nothing")
}
