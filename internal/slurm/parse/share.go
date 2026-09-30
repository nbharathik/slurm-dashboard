package parse

import (
	"strings"

	"github.com/nbharathik/slurm-dashboard/internal/model"
)

// Shares parses "sshare -U -n -P -o Account,User,RawShares,NormShares,RawUsage,EffectvUsage,FairShare".
// RawShares of "parent" is reported as -1.
func Shares(raw []byte) (shares []model.Share, warns []model.ParseWarning) {
	w := &warnings{source: "fairshare"}
	defer func() { warns = w.list }()
	defer w.recover()

	lines(raw, w, func(n int, line string) {
		f := cleanFields(strings.Split(line, "|"))
		if len(f) != 7 {
			w.add(n, line, "expected 7 fields, got %d", len(f))
			return
		}
		var fe fieldErr
		s := model.Share{
			Account:        strings.TrimSpace(f[0]),
			User:           strings.TrimSpace(f[1]),
			NormShares:     fe.atof(f[3]),
			RawUsage:       fe.atoi64(f[4]),
			EffectiveUsage: fe.atof(f[5]),
			FairShare:      fe.atof(f[6]),
		}
		if strings.EqualFold(strings.TrimSpace(f[2]), "parent") {
			s.RawShares = -1
		} else {
			s.RawShares = fe.atoi64(f[2])
		}
		if fe.err != nil {
			w.add(n, line, "%v", fe.err)
			return
		}
		shares = append(shares, s)
	})
	return shares, w.list
}

// Priorities parses "sprio -h -u $USER -o %i<Sep>%Y<Sep>%A<Sep>%F<Sep>%J<Sep>%P<Sep>%Q".
func Priorities(raw []byte) (rows []model.PriorityFactors, warns []model.ParseWarning) {
	w := &warnings{source: "sprio"}
	defer func() { warns = w.list }()
	defer w.recover()

	lines(raw, w, func(n int, line string) {
		f := cleanFields(strings.Split(line, Sep))
		if len(f) != 7 {
			w.add(n, line, "expected 7 fields, got %d", len(f))
			return
		}
		var fe fieldErr
		p := model.PriorityFactors{
			JobID:     strings.TrimSpace(f[0]),
			Priority:  fe.atoi64(f[1]),
			Age:       fe.atoi64(f[2]),
			FairShare: fe.atoi64(f[3]),
			JobSize:   fe.atoi64(f[4]),
			Partition: fe.atoi64(f[5]),
			QOS:       fe.atoi64(f[6]),
		}
		if fe.err != nil {
			w.add(n, line, "%v", fe.err)
			return
		}
		rows = append(rows, p)
	})
	return rows, w.list
}
