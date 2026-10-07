package ui

import "strings"

func viewDepends(view, source string) bool {
	deps := map[string]string{
		"overview": "myjobs alljobs nodes cluster partitions reservations storage fairshare history mystats queuerank",
		"jobs":     "myjobs cluster queuerank jobdetail sprio mystats",
		"queue":    "alljobs jobdetail sprio",
		"nodes":    "nodes cluster partitions reservations alljobs queuerank",
		"usage":    "history limits fairshare myjobs sprio",
		"storage":  "storage",
	}
	return strings.Contains(" "+deps[view]+" ", " "+source+" ")
}
