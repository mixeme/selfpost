package health

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// machineSampleWindow is the longest gap between two readings that still counts
// as "now". CPU busy time and network throughput are differences between two
// samples, and the status fragment polls every five seconds — but a page opened
// after the panel sat idle for an hour would otherwise average that whole hour
// and present it as the current load. Past this the reading only re-baselines,
// and the rates say they are still being measured.
const machineSampleWindow = time.Minute

// Thresholds for the two resources whose exhaustion actually threatens mail: a
// fully busy processor slows queue processing, and a machine out of memory has
// its processes killed. Network throughput has no comparable threshold — what
// counts as a lot depends entirely on the link — so it is reported, not graded.
const (
	cpuWarnPct  = 90.0
	memWarnPct  = 90.0
	memErrorPct = 97.0
)

// Machine is the resource usage of the machine this container runs on: the
// status page's answer to "is the server itself under strain", next to the
// component checks that answer "is mail flowing".
//
// The numbers come from the kernel's /proc filesystem, which a container shares
// with its host unless it was started with its own namespaces — so CPU and
// memory describe the host, while /proc/net/dev describes whatever network
// namespace the container is in (its own veth under the default bridge, the
// host's interfaces under network_mode: host).
type Machine struct {
	CPU     CPU
	Memory  Memory
	Network Network
	// Window is the interval the rates were measured over; zero until a
	// second reading exists.
	Window time.Duration
	Status Status
}

// WindowText names the sampling interval for the card's description.
func (m Machine) WindowText() string {
	if m.Window <= 0 {
		return ""
	}
	return m.Window.Round(time.Second).String()
}

// CPU is processor load over the sampling window.
type CPU struct {
	// Measured is false until two readings exist to compare; BusyPct means
	// nothing until it is true.
	Measured bool
	BusyPct  float64
	Cores    int
	// Load is the 1/5/15-minute load average, present when /proc/loadavg
	// could be read. Unlike BusyPct it needs no previous sample, so it is
	// there on the very first page load.
	Load    [3]float64
	HasLoad bool
	Status  Status
	Detail  string
}

// Percent is BusyPct as a whole number, for the <meter> element's value
// attribute. The bar carries its value in an attribute rather than a width in a
// style attribute because the panel's CSP has no inline-style exemption
// (security.md).
func (c CPU) Percent() int { return percent(c.BusyPct) }

// BusyText is the reading as it appears beside the bar.
func (c CPU) BusyText() string { return fmt.Sprintf("%d%%", c.Percent()) }

// Memory is main memory (and swap, where the machine has any) at the moment of
// the reading. Unlike CPU and network it is a level, not a rate, so a single
// reading is enough and it is never in the "measuring" state.
type Memory struct {
	Measured       bool
	TotalBytes     uint64
	AvailableBytes uint64
	UsedBytes      uint64
	UsedPct        float64
	SwapTotalBytes uint64
	SwapUsedBytes  uint64
	Status         Status
	Detail         string
}

// Percent, UsedText and TotalText render the reading for the template; see
// CPU.Percent for why the bar's value travels as an attribute.
func (m Memory) Percent() int      { return percent(m.UsedPct) }
func (m Memory) UsedText() string  { return humanBytes(m.UsedBytes) }
func (m Memory) TotalText() string { return humanBytes(m.TotalBytes) }
func (m Memory) PctText() string   { return fmt.Sprintf("%d%%", m.Percent()) }

// Interface is one network interface's traffic: the counters since the
// interface came up, and the throughput over the sampling window.
type Interface struct {
	Name     string
	RxBytes  uint64
	TxBytes  uint64
	RxRate   float64 // bytes per second, valid when the Network is Measured
	TxRate   float64
	Measured bool
}

func (i Interface) InText() string      { return humanBytes(i.RxBytes) }
func (i Interface) OutText() string     { return humanBytes(i.TxBytes) }
func (i Interface) InRateText() string  { return humanRate(i.RxRate) }
func (i Interface) OutRateText() string { return humanRate(i.TxRate) }

// Network is the traffic across every interface that has carried any, loopback
// excluded — loopback traffic is the container talking to itself (the panel to
// SQLite, Postfix to its milters) and says nothing about the link.
type Network struct {
	Measured   bool
	Interfaces []Interface
	RxRate     float64
	TxRate     float64
	Status     Status
	Detail     string
}

func (n Network) InRateText() string  { return humanRate(n.RxRate) }
func (n Network) OutRateText() string { return humanRate(n.TxRate) }

// MachineSampler reads those counters. Its zero value is ready to use and it is
// safe for concurrent use, but one sampler has to be shared by every caller:
// the rates are measured against the reading the previous call left behind, so
// a fresh sampler per request would never have anything to compare against.
type MachineSampler struct {
	// procRoot replaces /proc in tests; empty means the real one.
	procRoot string

	mu      sync.Mutex
	prevAt  time.Time
	prevCPU cpuTimes
	prevNet map[string]netCounters
}

func (m *MachineSampler) root() string {
	if m.procRoot == "" {
		return "/proc"
	}
	return m.procRoot
}

// cpuTimes is the aggregate of /proc/stat's "cpu" line: all time accounted for,
// and the part of it the processor spent doing nothing.
type cpuTimes struct {
	total uint64
	idle  uint64
}

// netCounters is one interface's byte counters from /proc/net/dev.
type netCounters struct {
	rx uint64
	tx uint64
}

// Sample reads the current counters and reports usage since the previous call.
// Like every other check here it never fails: a counter that cannot be read
// becomes an unknown status with an explanation, so a kernel that does not
// publish one of these files (or a panel run outside Linux for development)
// costs one line of the card rather than the page.
func (m *MachineSampler) Sample() Machine {
	root := m.root()
	now := time.Now()
	cpuNow, cores, cpuErr := readCPUTimes(root)
	netNow, netErr := readNetDev(root)

	m.mu.Lock()
	prevAt, prevCPU, prevNet := m.prevAt, m.prevCPU, m.prevNet
	if cpuErr == nil {
		m.prevCPU = cpuNow
	}
	if netErr == nil {
		m.prevNet = netNow
	}
	if cpuErr == nil || netErr == nil {
		m.prevAt = now
	}
	m.mu.Unlock()

	// A window of zero (two calls in the same instant) would divide by zero;
	// one longer than machineSampleWindow is no longer a description of now.
	window := now.Sub(prevAt)
	fresh := !prevAt.IsZero() && window > 0 && window <= machineSampleWindow

	mach := Machine{
		CPU:     cpuUsage(prevCPU, cpuNow, cores, readLoadAvg(root), fresh, cpuErr),
		Memory:  readMemory(root),
		Network: networkUsage(prevNet, netNow, window, fresh, netErr),
	}
	if fresh {
		mach.Window = window
	}
	mach.Status = Worst(mach.CPU.Status, mach.Memory.Status, mach.Network.Status)
	return mach
}

// cpuUsage grades the processor over the window. The load average is reported
// alongside it because the two answer different questions — how busy the
// processor was, and how many tasks were waiting for it — and a machine can
// look idle while work queues up behind a slow disk.
func cpuUsage(prev, cur cpuTimes, cores int, load [3]float64, fresh bool, err error) CPU {
	c := CPU{Cores: cores}
	if !isZeroLoad(load) {
		c.Load, c.HasLoad = load, true
	}
	if err != nil {
		c.Status = StatusUnknown
		c.Detail = "The kernel's processor counters (/proc/stat) could not be read here."
		return c
	}
	if !fresh || cur.total <= prev.total {
		c.Status = StatusUnknown
		c.Detail = joinDetail("Measuring — this reading sets the baseline; the next refresh has the figure.", c.contextText())
		return c
	}

	total := cur.total - prev.total
	idle := uint64(0)
	if cur.idle > prev.idle {
		idle = cur.idle - prev.idle
	}
	if idle > total {
		idle = total
	}
	c.Measured = true
	c.BusyPct = 100 * float64(total-idle) / float64(total)

	if c.BusyPct >= cpuWarnPct {
		c.Status = StatusWarn
		c.Detail = joinDetail(c.contextText(), "The processor is close to fully busy, which slows queue processing and every panel page.")
	} else {
		c.Status = StatusOK
		c.Detail = c.contextText()
	}
	return c
}

// contextText is the CPU's supporting figures: what the percentage is a
// percentage of, and how deep the run queue is.
func (c CPU) contextText() string {
	var parts []string
	if c.Cores > 0 {
		parts = append(parts, fmt.Sprintf("%d core(s)", c.Cores))
	}
	if c.HasLoad {
		parts = append(parts, fmt.Sprintf("load average %.2f, %.2f, %.2f", c.Load[0], c.Load[1], c.Load[2]))
	}
	return strings.Join(parts, " · ")
}

// readMemory reports main memory from /proc/meminfo. Used is derived from
// MemAvailable rather than MemFree: Linux spends every spare page on cache, so
// MemFree on a healthy machine is near zero and would report a permanent
// emergency. MemAvailable is the kernel's own estimate of what a new workload
// could actually get.
func readMemory(root string) Memory {
	var m Memory
	fields, err := readMeminfo(root)
	if err != nil {
		m.Status = StatusUnknown
		m.Detail = "The kernel's memory counters (/proc/meminfo) could not be read here."
		return m
	}
	total, available := fields["MemTotal"], fields["MemAvailable"]
	if total == 0 {
		m.Status = StatusUnknown
		m.Detail = "/proc/meminfo does not report a total memory size."
		return m
	}
	if available > total {
		available = total
	}

	m.Measured = true
	m.TotalBytes = total
	m.AvailableBytes = available
	m.UsedBytes = total - available
	m.UsedPct = 100 * float64(m.UsedBytes) / float64(total)
	m.SwapTotalBytes = fields["SwapTotal"]
	if swapFree := fields["SwapFree"]; m.SwapTotalBytes > swapFree {
		m.SwapUsedBytes = m.SwapTotalBytes - swapFree
	}

	detail := fmt.Sprintf("%s used of %s; %s available to new work.",
		humanBytes(m.UsedBytes), humanBytes(total), humanBytes(available))
	if m.SwapTotalBytes > 0 {
		detail += fmt.Sprintf(" Swap: %s of %s.", humanBytes(m.SwapUsedBytes), humanBytes(m.SwapTotalBytes))
	}
	switch {
	case m.UsedPct >= memErrorPct:
		m.Status = StatusError
		m.Detail = detail + " Memory is exhausted; the kernel kills processes to reclaim it, and Postfix or the panel are candidates."
	case m.UsedPct >= memWarnPct:
		m.Status = StatusWarn
		m.Detail = detail + " Little headroom left."
	default:
		m.Status = StatusOK
		m.Detail = detail
	}
	return m
}

// networkUsage turns two readings of the interface counters into throughput.
// It never grades: there is no usage figure that is wrong for a mail server, so
// the row is informational and only reports "unknown" when the counters are
// unreadable.
func networkUsage(prev, cur map[string]netCounters, window time.Duration, fresh bool, err error) Network {
	var n Network
	if err != nil {
		n.Status = StatusUnknown
		n.Detail = "The kernel's network counters (/proc/net/dev) could not be read here."
		return n
	}

	names := make([]string, 0, len(cur))
	for name := range cur {
		names = append(names, name)
	}
	// Map order is random, and this table is re-rendered every few seconds:
	// without a sort the rows would shuffle under the reader.
	sort.Strings(names)

	n.Measured = fresh
	for _, name := range names {
		c := cur[name]
		// An interface that has never carried a byte is a veth or a bridge
		// the deployment happens to have, not part of the mail path.
		if c.rx == 0 && c.tx == 0 {
			continue
		}
		iface := Interface{Name: name, RxBytes: c.rx, TxBytes: c.tx, Measured: fresh}
		if fresh {
			p := prev[name]
			secs := window.Seconds()
			// Counters only go up; a drop means the interface (or the
			// container) was recreated, so there is no rate to report.
			if c.rx >= p.rx {
				iface.RxRate = float64(c.rx-p.rx) / secs
			}
			if c.tx >= p.tx {
				iface.TxRate = float64(c.tx-p.tx) / secs
			}
			n.RxRate += iface.RxRate
			n.TxRate += iface.TxRate
		}
		n.Interfaces = append(n.Interfaces, iface)
	}

	n.Status = StatusOK
	switch {
	case len(n.Interfaces) == 0:
		n.Detail = "No interface outside loopback has carried any traffic."
	case !fresh:
		n.Detail = "Measuring — this reading sets the baseline; the next refresh has the throughput."
	}
	return n
}

// readCPUTimes returns the aggregate processor times and the number of cores
// from /proc/stat. The times are in USER_HZ ticks, which cancel out because
// only their ratio is used.
func readCPUTimes(root string) (cpuTimes, int, error) {
	data, err := os.ReadFile(filepath.Join(root, "stat"))
	if err != nil {
		return cpuTimes{}, 0, err
	}
	var (
		times cpuTimes
		cores int
		found bool
	)
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 5 || !strings.HasPrefix(fields[0], "cpu") {
			continue
		}
		if fields[0] != "cpu" {
			cores++ // cpu0, cpu1, … — one line per core
			continue
		}
		// user nice system idle iowait irq softirq steal (guest fields are
		// already counted inside user/nice, so they are left out).
		for i, f := range fields[1:] {
			if i >= 8 {
				break
			}
			v, err := strconv.ParseUint(f, 10, 64)
			if err != nil {
				return cpuTimes{}, 0, fmt.Errorf("/proc/stat: unreadable cpu time %q", f)
			}
			times.total += v
			// idle plus iowait: both are time the processor had nothing to
			// run, and separating them tells the reader nothing here.
			if i == 3 || i == 4 {
				times.idle += v
			}
		}
		found = true
	}
	if !found {
		return cpuTimes{}, 0, fmt.Errorf("/proc/stat: no aggregate cpu line")
	}
	return times, cores, nil
}

// readLoadAvg reads the 1/5/15-minute load averages. A machine without
// /proc/loadavg simply has none reported, so the failure is a zero value rather
// than an error.
func readLoadAvg(root string) [3]float64 {
	var load [3]float64
	data, err := os.ReadFile(filepath.Join(root, "loadavg"))
	if err != nil {
		return load
	}
	fields := strings.Fields(string(data))
	if len(fields) < 3 {
		return load
	}
	for i := range load {
		v, err := strconv.ParseFloat(fields[i], 64)
		if err != nil {
			return [3]float64{}
		}
		load[i] = v
	}
	return load
}

func isZeroLoad(load [3]float64) bool {
	return load == [3]float64{}
}

// readMeminfo returns the /proc/meminfo entries this package uses, in bytes.
// The file reports kB (kibibytes, despite the label) for these fields.
func readMeminfo(root string) (map[string]uint64, error) {
	data, err := os.ReadFile(filepath.Join(root, "meminfo"))
	if err != nil {
		return nil, err
	}
	want := map[string]bool{"MemTotal": true, "MemAvailable": true, "SwapTotal": true, "SwapFree": true}
	out := make(map[string]uint64, len(want))
	for _, line := range strings.Split(string(data), "\n") {
		name, rest, ok := strings.Cut(line, ":")
		if !ok || !want[name] {
			continue
		}
		fields := strings.Fields(rest)
		if len(fields) == 0 {
			continue
		}
		v, err := strconv.ParseUint(fields[0], 10, 64)
		if err != nil {
			continue
		}
		if len(fields) > 1 && strings.EqualFold(fields[1], "kB") {
			v *= 1024
		}
		out[name] = v
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("/proc/meminfo: no readable fields")
	}
	return out, nil
}

// readNetDev returns each interface's byte counters from /proc/net/dev, loopback
// excluded. Each line is "  eth0: <rx bytes> <rx packets> … <tx bytes> …" — the
// name is separated by a colon, which may or may not have a space after it, so
// the split is on the colon and not on whitespace.
func readNetDev(root string) (map[string]netCounters, error) {
	data, err := os.ReadFile(filepath.Join(root, "net", "dev"))
	if err != nil {
		return nil, err
	}
	out := make(map[string]netCounters)
	for _, line := range strings.Split(string(data), "\n") {
		name, rest, ok := strings.Cut(line, ":")
		name = strings.TrimSpace(name)
		if !ok || name == "" || name == "lo" || strings.Contains(name, " ") {
			continue // header lines carry no name, or several words
		}
		fields := strings.Fields(rest)
		if len(fields) < 9 {
			continue
		}
		rx, err1 := strconv.ParseUint(fields[0], 10, 64)
		tx, err2 := strconv.ParseUint(fields[8], 10, 64)
		if err1 != nil || err2 != nil {
			continue
		}
		out[name] = netCounters{rx: rx, tx: tx}
	}
	return out, nil
}

// percent rounds a 0–100 reading to a whole number and clamps it, so a bar's
// value attribute is always inside the range the element declares.
func percent(v float64) int {
	switch {
	case v <= 0:
		return 0
	case v >= 100:
		return 100
	default:
		return int(v + 0.5)
	}
}

// humanBytes renders a byte count in the binary units memory and traffic are
// conventionally read in.
func humanBytes(b uint64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := uint64(unit), 0
	for n := b / unit; n >= unit && exp < 4; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(b)/float64(div), "KMGTP"[exp])
}

// humanRate renders throughput in bytes per second, to match the totals beside
// it rather than the bits per second a link is sold in.
func humanRate(perSec float64) string {
	if perSec < 0 {
		perSec = 0
	}
	return humanBytes(uint64(perSec+0.5)) + "/s"
}

// joinDetail joins the non-empty parts of a detail line.
func joinDetail(parts ...string) string {
	var kept []string
	for _, p := range parts {
		if p != "" {
			kept = append(kept, p)
		}
	}
	return strings.Join(kept, " ")
}
