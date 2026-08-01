// Package health reports the running container's own operating state for the
// panel's status screen: the supervised processes, the TLS certificate Postfix
// serves, and the milter sockets delivery depends on.
//
// Every check is read-only and reports a problem as a Status value rather than
// an error return, so one broken component degrades a single line of the status
// page instead of blanking the whole thing. The package also owns the Status
// vocabulary shared with internal/dnscheck, so the panel renders every check —
// local or DNS — through one set of badges.
package health

// Status is the outcome of a single check, in the order the status page treats
// them: unknown < ok < warn < error, worst wins for a group.
type Status string

const (
	// StatusUnknown means the check could not be performed at all (a missing
	// setting, an unreachable resolver) — not evidence of a problem.
	StatusUnknown Status = "unknown"
	// StatusOK means the checked component is in its expected state.
	StatusOK Status = "ok"
	// StatusWarn means something is off but mail still flows.
	StatusWarn Status = "warn"
	// StatusError means mail delivery is (or soon will be) affected.
	StatusError Status = "error"
)

// severity orders statuses so a group can report its worst member.
func (s Status) severity() int {
	switch s {
	case StatusError:
		return 3
	case StatusWarn:
		return 2
	case StatusOK:
		return 1
	default:
		return 0
	}
}

// Worst returns the most severe of the given statuses, or StatusUnknown when
// there are none. It is how the status page rolls a list of checks up into one
// headline.
func Worst(statuses ...Status) Status {
	worst := StatusUnknown
	for _, s := range statuses {
		if s.severity() > worst.severity() {
			worst = s
		}
	}
	return worst
}
