package parse

import (
	"cmp"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/nbharathik/slurm-dashboard/internal/model"
	"github.com/nbharathik/slurm-dashboard/internal/textsafe"
)

// maxRPCRows is how many RPC types and users are kept.
const maxRPCRows = 8

var (
	epochRe = regexp.MustCompile(`\((\d{9,11})\)\s*$`)
	// rpcRe is "NAME ( 2009) count:52310 ave_time:212 total_time:11090100".
	rpcRe = regexp.MustCompile(`^(\S+)\s*\(\s*\d+\)\s+count:(\d+)\s+ave_time:(\d+)\s+total_time:(\d+)`)
)

// Sdiag parses sdiag's report. Unknown lines are skipped silently, as each
// Slurm release adds some; a report with no known line is a warning.
func Sdiag(raw []byte) (s model.SchedStats, warns []model.ParseWarning) {
	w := &warnings{source: "sdiag"}
	defer func() { warns = w.list }()
	defer w.recover()

	section := ""
	known := 0
	epoch := func(v string) time.Time {
		if m := epochRe.FindStringSubmatch(v); m != nil {
			if n, err := strconv.ParseInt(m[1], 10, 64); err == nil {
				return time.Unix(n, 0)
			}
		}
		return time.Time{}
	}
	lines(raw, w, func(n int, line string) {
		trim := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trim, "sdiag output at"):
			s.At, known = epoch(trim), known+1
			return
		case strings.HasPrefix(trim, "Data since"):
			s.Since = epoch(trim)
			return
		case strings.HasPrefix(trim, "Main schedule statistics"):
			section = "main"
			return
		case strings.HasPrefix(trim, "Backfilling stats"):
			section = "backfill"
			return
		case strings.HasPrefix(trim, "Remote Procedure Call statistics by message type"):
			section = "rpctype"
			return
		case strings.HasPrefix(trim, "Remote Procedure Call statistics by user"):
			section = "rpcuser"
			return
		case strings.HasPrefix(trim, "Pending RPC statistics"), strings.HasPrefix(trim, "Latency for"):
			section = "other"
			return
		}
		if section == "rpctype" || section == "rpcuser" {
			m := rpcRe.FindStringSubmatch(trim)
			if m == nil {
				return
			}
			var fe fieldErr
			r := model.RPCStat{Name: own(textsafe.Field(m[1])), Count: fe.atoi64(m[2]), AveUS: fe.atoi64(m[3]), TotalU: fe.atoi64(m[4])}
			if fe.err != nil {
				w.add(n, line, "%v", fe.err)
				return
			}
			if section == "rpctype" {
				s.RPCTypes = append(s.RPCTypes, r)
			} else {
				s.RPCUsers = append(s.RPCUsers, r)
			}
			known++
			return
		}
		key, val, ok := strings.Cut(trim, ":")
		if !ok {
			return
		}
		key, val = strings.TrimSpace(key), strings.TrimSpace(val)
		num := func() int64 {
			f, _, _ := strings.Cut(val, " ")
			v, err := strconv.ParseInt(f, 10, 64)
			if err != nil {
				return 0
			}
			return v
		}
		i := func() int { return int(num()) }
		known++
		switch section {
		case "main":
			switch key {
			case "Last cycle":
				s.Main.LastUS = num()
			case "Max cycle":
				s.Main.MaxUS = num()
			case "Mean cycle":
				s.Main.MeanUS = num()
			case "Mean depth cycle":
				s.Main.MeanDepth = i()
			case "Cycles per minute":
				s.Main.PerMinute = i()
			case "Last queue length":
				s.Main.LastQueue = i()
			default:
				known--
			}
		case "backfill":
			switch key {
			case "Total backfilled jobs (since last slurm start)":
				s.Backfill.JobsTotal = i()
			case "Total backfilled jobs (since last stats cycle start)":
				s.Backfill.Jobs = i()
			case "Total cycles":
				s.Backfill.Cycles = i()
			case "Last cycle when":
				s.Backfill.LastAt = epoch(val)
			case "Last cycle":
				s.Backfill.LastUS = num()
			case "Max cycle":
				s.Backfill.MaxUS = num()
			case "Mean cycle":
				s.Backfill.MeanUS = num()
			case "Last depth cycle":
				s.Backfill.LastDepth = i()
			case "Last depth cycle (try sched)":
				s.Backfill.LastTried = i()
			case "Depth Mean":
				s.Backfill.MeanDepth = i()
			case "Last queue length":
				s.Backfill.QueueLength = i()
			default:
				known--
			}
		case "":
			switch key {
			case "Server thread count":
				s.ServerThreads = i()
			case "Agent queue size":
				s.AgentQueue = i()
			case "DBD Agent queue size":
				s.DBDQueue = i()
			case "Jobs submitted":
				s.Submitted = i()
			case "Jobs started":
				s.Started = i()
			case "Jobs completed":
				s.Completed = i()
			case "Jobs canceled":
				s.Cancelled = i()
			case "Jobs failed":
				s.Failed = i()
			case "Jobs pending":
				s.Pending = i()
			case "Jobs running":
				s.Running = i()
			default:
				known--
			}
		default:
			known--
		}
	})
	if known == 0 && len(strings.TrimSpace(string(raw))) > 0 {
		w.add(1, firstOf(raw), "no sdiag statistics found")
	}
	busiest := func(a, b model.RPCStat) int {
		return cmp.Or(cmp.Compare(b.TotalU, a.TotalU), strings.Compare(a.Name, b.Name))
	}
	slices.SortFunc(s.RPCTypes, busiest)
	slices.SortFunc(s.RPCUsers, busiest)
	s.RPCTypes = s.RPCTypes[:min(len(s.RPCTypes), maxRPCRows)]
	s.RPCUsers = s.RPCUsers[:min(len(s.RPCUsers), maxRPCRows)]
	return s, w.list
}

func firstOf(raw []byte) string {
	l, _, _ := strings.Cut(string(raw), "\n")
	return l
}
