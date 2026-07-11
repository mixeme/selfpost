// Command panel is the SelfPost control panel. In the finished product this
// single binary combines several roles (spec 7.1): the HTTP panel server,
// the journal-milter, the mail.log tailer and the rate-limit checks.
//
// This is the Phase 0 skeleton: it only reports its version so the build
// pipeline (ldflags stamping, docker build) can be wired up end to end.
package main

import (
	"flag"
	"fmt"
	"os"

	"codeberg.org/mix/selfpost/internal/buildinfo"
)

func main() {
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println(buildinfo.Version)
		return
	}

	fmt.Fprintf(os.Stdout, "selfpost panel %s (skeleton)\n", buildinfo.Version)
}
