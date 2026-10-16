NR == 1 {
	printf "%-10s %-9s %-11s %-2s %-9s %s\n", "JOBID", "PARTITION", "NAME", "ST", "TIME", "NODELIST(REASON)"
	next
}
{
	st = "R"
	where = $7
	if ($3 == "PENDING") {
		st = "PD"
		sub(/^#[0-9]+ of [0-9]+ [^ ]+ /, "", where)
		where = "(" where ")"
	}
	split($4, t, "/")
	printf "%-10s %-9s %-11s %-2s %-9s %s\n", $1, $6, $2, st, t[1], where
}
