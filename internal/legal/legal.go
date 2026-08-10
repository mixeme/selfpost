// Package legal embeds SelfPost's AGPL-3.0 licence text so the panel can
// serve it at /license (Appropriate Legal Notices) without depending on
// GitHub or a file on disk at runtime.
//
// The copy in this directory must stay identical to the repository-root
// LICENSE; license_test.go enforces that.
package legal

import (
	_ "embed"
)

//go:embed LICENSE
var License []byte

// SourceURL is where Corresponding Source for the published upstream
// releases lives. Operators who ship a modified version must point their
// users at their own sources instead (NOTICE; AGPL-3.0 §13).
const SourceURL = "https://github.com/mixeme/selfpost"

// CopyrightLine is the short copyright notice shown in the panel footer.
const CopyrightLine = "Copyright © 2026 Mikhail Yenuchenko"
