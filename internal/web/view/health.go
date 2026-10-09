package view

// The data of Server › Health (templates/health.html, mockup health.html):
// every check behind the cards of Overview, as tables, and the home of the two
// server actions, Re-check DNS and Reload configuration.
//
// Two rows of the page are polled (templates/health_body.html): the handler
// builds the same Health for the page and for its fragment, and the fragment
// carries Refresh so that its second row is swapped in out of band.

// Health is the data of the Health page.
type Health struct {
	Meta
	Head  Head
	Flash *Flash
	// Refresh is set on the polled fragment only. The fragment answers with two
	// sibling rows; the first replaces the element that polls, and the second
	// is marked to be swapped into place by its id.
	Refresh bool

	Machine     Box
	MachineRows []MachineRow

	Processes    Box
	ProcessRows  []ProcessRow
	ProcessError bool

	Certificate Box
	CertFacts   []Fact
	CertDetail  string
	CertProblem bool

	Sockets       Box
	SocketRows    []SocketRow
	Hostname      Box
	HostFacts     []Fact
	Configuration Box
	Reload        Action
}

// MachineRow is one line of the Machine table. A row that is a level (CPU,
// memory) carries a Gauge; one that is a rate (network) carries Usage text; a
// reading that could not be taken carries neither and shows a dash. Detail is
// the supporting text, one line each.
type MachineRow struct {
	Resource string
	Gauge    *Gauge
	Usage    string
	Detail   []string
}

// Gauge is a reading shown as a bar and its figure: Percent fills the bar, Text
// is the figure printed beside it, and Level (ok, warn or fail) colours it.
type Gauge struct {
	Percent int
	Text    string
	Level   string
}

// Bar is the Bulma class that colours the gauge's progress bar.
func (g Gauge) Bar() string {
	switch g.Level {
	case LevelFail:
		return "is-danger"
	case LevelWarn:
		return "is-warning"
	}
	return "is-success"
}

// ProcessRow is one supervised program: its name, its state as a tag and what
// supervisord said about it.
type ProcessRow struct {
	Name   string
	State  Tag
	Detail string
}

// SocketRow is one milter socket. Note qualifies the name ("inbound spam
// filter"); Detail is shown only when the socket is not fine, since "Listening"
// says nothing the tag does not.
type SocketRow struct {
	Name   string
	Note   string
	Path   string
	State  Tag
	Detail string
}

// NewHealth builds the page around its content. The handler fills the tables;
// the boxes, the head and the reload action are the page's own.
func NewHealth(m Meta, flash string) *Health {
	m.Title, m.Section, m.Page = "Health", "server", "health"
	h := &Health{
		Meta: m,
		Head: Head{
			Kicker:  "Server",
			Title:   "Health",
			Lead:    Plain("Everything behind the cards on Overview. Refreshes automatically."),
			Actions: []Action{{Label: "Re-check DNS", Icon: "ti-refresh", Post: "/server/health/recheck"}},
		},
		Machine:     Box{No: "01", Title: "Machine", ID: "machine"},
		Processes:   Box{No: "02", Title: "Processes", ID: "processes"},
		Certificate: Box{No: "03", Title: "TLS certificate", ID: "certificate"},
		Sockets:     Box{No: "04", Title: "Milter sockets", ID: "sockets"},
		Hostname:    Box{No: "05", Title: "Hostname and reverse DNS", ID: "hostname"},
		Configuration: Box{No: "06", Title: "Configuration", ID: "configuration",
			End: Plain("Safe to run at any time")},
		Reload: Action{Label: "Reload configuration", Icon: "ti-reload", Post: "/server/health/reload"},
	}
	if flash != "" {
		h.Flash = &Flash{Text: Plain(flash)}
	}
	return h
}

// Verdict puts a check's status in the end slot of its box, as the mockup does.
func Verdict(status string) Text { return Text{TagOf(status, "")} }
