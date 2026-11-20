package parse

import "strings"

// Field positions in the jobs format; the order is JobFormatCodes'.
// Name stays last, as a job name may contain anything.
const (
	fJobID = iota
	fArrayJobID
	fArrayTask
	fPartition
	fQOS
	fAccount
	fState
	fTimeUsed
	fTimeLimit
	fTimeLeft
	fNodes
	fCPUs
	fMem
	fGRES
	fReason
	fNodeList
	fStart
	fEnd
	fSubmit
	fPriority
	fDependency
	fUser
	fName
	jobFieldCount
)

// jobFormatCodes are the squeue -o codes, one per field above.
var jobFormatCodes = [jobFieldCount]string{
	fJobID: "%i", fArrayJobID: "%F", fArrayTask: "%K", fPartition: "%P", fQOS: "%q",
	fAccount: "%a", fState: "%T", fTimeUsed: "%M", fTimeLimit: "%l", fTimeLeft: "%L",
	fNodes: "%D", fCPUs: "%C", fMem: "%m", fGRES: "%b", fReason: "%r", fNodeList: "%N",
	fStart: "%S", fEnd: "%e", fSubmit: "%V", fPriority: "%Q", fDependency: "%E",
	fUser: "%u", fName: "%j",
}

// JobsFormat is the squeue -o format MyJobs reads.
func JobsFormat() string { return strings.Join(jobFormatCodes[:], Sep) }
