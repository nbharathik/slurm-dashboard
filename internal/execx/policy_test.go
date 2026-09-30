package execx

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestClassify(t *testing.T) {
	us := "\x1f"
	readOnly := [][]string{
		{"sinfo", "--version"},
		{"scontrol", "show", "config"},
		{"squeue", "--me", "-h", "-o", "%i" + us + "%F" + us + "%j"},
		{"squeue", "-h", "-t", "RUNNING", "-O", "JobID:48,UserName:48"},
		{"squeue", "-h", "-t", "PENDING", "-p", "gpu,cpu", "-O", "JobID:48,Partition:64,PriorityLong:24"},
		{"scontrol", "show", "node", "-o"},
		{"scontrol", "show", "partition", "-o"},
		{"scontrol", "show", "reservation", "-o"},
		{"scontrol", "show", "job", "-o", "812"},
		{"scontrol", "--version"},
		{"scontrol", "-V"},
		{"sstat", "-a", "-n", "-P", "-j", "812", "-o", "JobID,NTasks,MaxRSS"},
		{"sacct", "-n", "-P", "--delimiter=" + us, "-u", "alice", "-S", "now-7days", "-E", "now", "-o", "JobID"},
		{"sacct", "-n", "-P", "-X", "-j", "812", "-o", "JobID,State,ExitCode,ElapsedRaw"},
		{"sshare", "-U", "-n", "-P", "-o", "Account,User"},
		{"sprio", "-h", "-u", "alice", "-o", "%i"},
		{"scontrol", "write", "batch_script", "812", "-"},
		{"sbatch", "--test-only"},
		{"lfs", "quota", "-q", "-u", "alice", "/lustre"},
		{"mmlsquota", "-u", "alice", "-Y", "--block-size", "1K"},
		{"/usr/lpp/mmfs/bin/mmlsquota", "-u", "alice"},
		{"beegfs-ctl", "--getquota", "--uid", "alice", "--csv"},
		{"quota", "-w", "-u", "alice"},
		{"id", "-un"},
		{"nice", "-n", "19", "ionice", "-c3", "du", "-x", "-d1", "-k", "--", "/scratch/alice"},
		{"nice", "-n19", "du", "-sk", "/home"},
		{"ionice", "-c", "2", "-n", "7", "du", "/x"},
		{"du", "-k", "/x"},
		{"cosign", "verify-blob", "--bundle", "b.json", "checksums.txt"},
		{"gh", "attestation", "verify", "sdash.tar.gz", "--repo", "o/r"},
	}
	for _, argv := range readOnly {
		if got := Classify(argv); got != ReadOnly {
			t.Errorf("Classify(%q) = %v, want read-only", argv, got)
		}
	}

	mutating := [][]string{
		nil,
		{},
		{"scancel", "812"},
		{"scancel", "--signal=USR1", "812"},
		{"scontrol", "hold", "812"},
		{"scontrol", "release", "812"},
		{"scontrol", "requeue", "812"},
		{"scontrol", "update", "JobId=812", "TimeLimit=1:00:00"},
		{"scontrol", "top", "812"},
		{"scontrol", "suspend", "812"},
		{"scontrol", "reconfigure"},
		{"scontrol", "-o", "show", "node"},           // leading option: fail closed
		{"scontrol", "write", "batch_script", "812"}, // writes a file
		{"scontrol", "write", "batch_script", "812", "out.sh"},
		{"scontrol", "write", "config"},
		{"scontrol"},
		{"sbatch", "--parsable"},
		{"sbatch", "job.sh", "--test-only"}, // --test-only after the script goes to the script
		{"sbatch", "-p", "gpu", "--test-only"},
		{"srun", "--jobid=812", "--overlap", "--pty", "bash", "-l"},
		{"salloc", "-N1"},
		{"lfs", "setstripe", "-c", "4", "/lustre/x"},
		{"lfs"},
		{"beegfs-ctl", "--setquota"},
		{"rm", "-rf", "/"},
		{"sh", "-c", "squeue"},
	}
	for _, argv := range mutating {
		if got := Classify(argv); got != Mutating {
			t.Errorf("Classify(%q) = %v, want mutating", argv, got)
		}
	}
	for _, argv := range [][]string{
		{"nice", "-n", "19", "rm", "-rf", "/"},
		{"nice", "ionice", "-c3", "sh", "-c", "du"},
		{"ionice", "-c3", "nice", "du"},
		{"nice", "-n", "x", "du"},
		{"nice"},
	} {
		if got := Classify(argv); got != Mutating {
			t.Errorf("Classify(%q) = %v, want mutating", argv, got)
		}
		if _, err := (Policy{}).Check(context.Background(), argv); !errors.Is(err, ErrNotAllowed) {
			t.Errorf("Check(%q) = %v, want refusal", argv, err)
		}
		// Not even a grant for the exact argv lets the wrappers run
		// something other than du.
		if _, err := (Policy{}).Check(WithMutation(context.Background(), "test", argv), argv); !errors.Is(err, ErrNotAllowed) {
			t.Errorf("Check(%q) with a grant = %v, want refusal", argv, err)
		}
	}
}

func TestPolicyCheck(t *testing.T) {
	authorised := WithMutation(context.Background(), "cancel", []string{"scancel", "812"})
	cases := []struct {
		name   string
		policy Policy
		ctx    context.Context
		argv   []string
		want   error
	}{
		{"empty", Policy{}, context.Background(), nil, ErrInvalidArgv},
		{"empty name", Policy{}, context.Background(), []string{""}, ErrInvalidArgv},
		{"relative path", Policy{AllowClusterToolsInTests: true}, context.Background(), []string{"./squeue"}, ErrInvalidArgv},
		{"relative dir", Policy{AllowClusterToolsInTests: true}, context.Background(), []string{"bin/squeue"}, ErrInvalidArgv},
		{"unclean abs", Policy{AllowClusterToolsInTests: true}, context.Background(), []string{"/usr/../bin/squeue"}, ErrInvalidArgv},
		{"newline", Policy{AllowClusterToolsInTests: true}, context.Background(), []string{"squeue", "-u", "alice\nbob"}, ErrInvalidArgv},
		{"nul", Policy{AllowClusterToolsInTests: true}, context.Background(), []string{"squeue", "a\x00b"}, ErrInvalidArgv},
		{"not allowlisted", Policy{}, context.Background(), []string{"rm", "-rf", "/"}, ErrNotAllowed},
		{"shell", Policy{}, context.Background(), []string{"/bin/sh", "-c", "squeue"}, ErrNotAllowed},
		{"injection in name", Policy{}, context.Background(), []string{"squeue;rm"}, ErrNotAllowed},
		{"salloc", Policy{AllowClusterToolsInTests: true}, context.Background(), []string{"salloc"}, ErrNotAllowed},
		{"sacctmgr", Policy{AllowClusterToolsInTests: true}, context.Background(), []string{"sacctmgr", "-i", "delete", "user", "bob"}, ErrNotAllowed},
		{"test guard read-only", Policy{}, context.Background(), []string{"squeue", "--me"}, ErrClusterToolInTests},
		{"test guard quota", Policy{}, context.Background(), []string{"quota", "-w"}, ErrClusterToolInTests},
		{"read-only ok", Policy{AllowClusterToolsInTests: true}, context.Background(), []string{"squeue", "--me"}, nil},
		{"abs path ok", Policy{AllowClusterToolsInTests: true}, context.Background(), []string{"/usr/bin/squeue", "--me"}, nil},
		{"mutating without grant", Policy{AllowClusterToolsInTests: true}, context.Background(), []string{"scancel", "812"}, ErrMutationNotAuthorized},
		{"mutating with grant", Policy{AllowClusterToolsInTests: true}, authorised, []string{"scancel", "812"}, nil},
		{"grant is argv-exact", Policy{AllowClusterToolsInTests: true}, authorised, []string{"scancel", "813"}, ErrMutationNotAuthorized},
		{"grant does not widen", Policy{AllowClusterToolsInTests: true}, authorised, []string{"scancel", "812", "813"}, ErrMutationNotAuthorized},
		{"grant is not a hold", Policy{AllowClusterToolsInTests: true}, authorised, []string{"scontrol", "hold", "812"}, ErrMutationNotAuthorized},
		{"cosign verify", Policy{}, context.Background(), []string{"cosign", "verify-blob", "--bundle", "b", "f"}, nil},
		{"gh attestation", Policy{}, context.Background(), []string{"gh", "attestation", "verify", "f", "--repo", "o/r"}, nil},
		{"cosign sign", Policy{}, authorised, []string{"cosign", "sign-blob", "f"}, ErrNotAllowed},
		{"gh other", Policy{}, context.Background(), []string{"gh", "repo", "delete", "o/r"}, ErrNotAllowed},
		{"gh grant does not widen", Policy{}, WithMutation(context.Background(), "x", []string{"gh", "api", "-X", "DELETE", "/x"}), []string{"gh", "api", "-X", "DELETE", "/x"}, ErrNotAllowed},
		{"extra read-only", Policy{ExtraReadOnly: []string{"myquota"}}, context.Background(), []string{"myquota", "--json"}, nil},
		{"extra cannot reclassify", Policy{ExtraReadOnly: []string{"scancel"}, AllowClusterToolsInTests: true}, context.Background(), []string{"scancel", "812"}, ErrMutationNotAuthorized},
		{"extra cannot add salloc", Policy{ExtraReadOnly: []string{"salloc"}}, context.Background(), []string{"salloc"}, ErrNotAllowed},
		{"fake skips test guard", Policy{NoTestGuard: true}, context.Background(), []string{"squeue", "--me"}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tc.policy.Check(tc.ctx, tc.argv)
			if tc.want == nil {
				if err != nil {
					t.Fatalf("Check(%q) = %v, want nil", tc.argv, err)
				}
				return
			}
			if !errors.Is(err, tc.want) {
				t.Fatalf("Check(%q) = %v, want %v", tc.argv, err, tc.want)
			}
		})
	}
}

func TestWithMutationCopiesArgv(t *testing.T) {
	argv := []string{"scancel", "812"}
	ctx := WithMutation(context.Background(), "cancel", argv)
	argv[1] = "813" // the caller mutating its slice must not widen the grant
	if _, ok := MutationFrom(ctx, []string{"scancel", "813"}); ok {
		t.Fatal("grant followed a later change to the caller's slice")
	}
	action, ok := MutationFrom(ctx, []string{"scancel", "812"})
	if !ok || action != "cancel" {
		t.Fatalf("MutationFrom = %q, %v", action, ok)
	}
}

func TestIsSlurmTool(t *testing.T) {
	for _, name := range []string{"squeue", "/opt/slurm/bin/sbatch", "salloc"} {
		if !IsSlurmTool(name) {
			t.Errorf("IsSlurmTool(%q) = false", name)
		}
	}
	for _, name := range []string{"vim", "lfs", "sh"} {
		if IsSlurmTool(name) {
			t.Errorf("IsSlurmTool(%q) = true", name)
		}
	}
}

func TestClassString(t *testing.T) {
	if ReadOnly.String() != "read-only" || !strings.Contains(Mutating.String(), "mut") {
		t.Fatal("Class.String")
	}
}
