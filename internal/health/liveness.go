package health

import (
	"fmt"
	"strings"
)

// mailPathPrograms are the supervised processes whose absence means the
// container should not report healthy to orchestrators.
var mailPathPrograms = map[string]bool{
	"opendkim": true,
	"panel":    true,
	"postfix":  true,
}

// Liveness reports whether the mail path is healthy enough for container
// probes. It requires opendkim, panel, and postfix to be RUNNING under
// supervisord.
func Liveness() error {
	procs, err := Processes()
	if err != nil {
		return err
	}

	seen := make(map[string]Status, len(mailPathPrograms))
	for _, p := range procs {
		if mailPathPrograms[p.Name] {
			seen[p.Name] = p.Status
		}
	}

	var unhealthy []string
	for name := range mailPathPrograms {
		switch seen[name] {
		case StatusOK:
		case StatusUnknown:
			unhealthy = append(unhealthy, name+": missing")
		default:
			unhealthy = append(unhealthy, fmt.Sprintf("%s: %s", name, seen[name]))
		}
	}
	if len(unhealthy) > 0 {
		return fmt.Errorf("unhealthy: %s", strings.Join(unhealthy, ", "))
	}
	return nil
}
