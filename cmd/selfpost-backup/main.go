// Command selfpost-backup produces (and helps restore) the full persistent-state
// archive from inside the container, invoked via `docker exec` for scripted/cron
// backups — the CLI equivalent of the panel's backup button (spec 7.5.A, 11.6).
//
// This is the Phase 0 skeleton: it only reports its version. The actual archive
// logic lands in Phase 9.
package main

import (
	"flag"
	"fmt"

	"codeberg.org/mix/selfpost/internal/buildinfo"
)

func main() {
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println(buildinfo.Version)
		return
	}

	fmt.Printf("selfpost-backup %s (skeleton)\n", buildinfo.Version)
}
