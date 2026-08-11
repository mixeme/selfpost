package health

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// fakeProc writes a /proc-shaped directory the sampler can be pointed at, so
// the parsing and the arithmetic are tested against known counters instead of
// whatever the machine running the tests happens to be doing.
func fakeProc(t *testing.T, stat, meminfo, loadavg, netdev string) string {
	t.Helper()
	dir := t.TempDir()
	write := func(name, body string) {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("stat", stat)
	write("meminfo", meminfo)
	write("loadavg", loadavg)
	write(filepath.Join("net", "dev"), netdev)
	return dir
}

const meminfoSample = `MemTotal:        4194304 kB
MemFree:          131072 kB
MemAvailable:    2097152 kB
Buffers:          262144 kB
SwapTotal:       1048576 kB
SwapFree:         524288 kB
`

const netdevSample = `Inter-|   Receive                                                |  Transmit
 face |bytes    packets errs drop fifo frame compressed multicast|bytes    packets errs drop fifo colls carrier compressed
    lo:  500000    1000    0    0    0     0          0         0   500000    1000    0    0    0     0       0          0
  eth0: 1048576    2000    0    0    0     0          0         0   524288    1500    0    0    0     0       0          0
 veth9:       0       0    0    0    0     0          0         0        0       0    0    0    0     0       0          0
`

func TestCPUUsageOverTwoReadings(t *testing.T) {
	// 1000 ticks pass, 250 of them idle: 75% busy.
	prev := cpuTimes{total: 10000, idle: 8000}
	cur := cpuTimes{total: 11000, idle: 8250}
	got := cpuUsage(prev, cur, 4, 8, [3]float64{0.5, 0.4, 0.3}, true, nil)
	if !got.Measured {
		t.Fatalf("reading not marked measured: %+v", got)
	}
	if got.Percent() != 75 {
		t.Errorf("busy = %.2f%% (%s), want 75%%", got.BusyPct, got.BusyText())
	}
	if got.Status != StatusOK {
		t.Errorf("status = %q, want ok", got.Status)
	}
	if got.Cores != 4 || got.Threads != 8 || !got.HasLoad {
		t.Errorf("cores/threads/load not reported: %+v", got)
	}
	if got.Detail != "4 cores · 8 threads" {
		t.Errorf("detail = %q, want cores and threads only", got.Detail)
	}
}

func TestCPUUsageWarnsWhenFullyBusy(t *testing.T) {
	got := cpuUsage(cpuTimes{total: 10000, idle: 5000}, cpuTimes{total: 11000, idle: 5010}, 1, 1, [3]float64{}, true, nil)
	if got.Status != StatusWarn {
		t.Errorf("99%% busy graded %q, want warn (%s)", got.Status, got.Detail)
	}
	if got.Detail != "1 cores · 1 threads" {
		t.Errorf("detail = %q, want topology only (no warn prose)", got.Detail)
	}
	if got.HasLoad {
		t.Error("a missing load average should not be reported as zeros")
	}
}

// The first reading has nothing to compare against, and one taken after a long
// idle stretch would describe that stretch rather than now. Both must report
// "unknown" rather than a number the reader would take for the current load.
func TestCPUUsageWithoutAUsableWindow(t *testing.T) {
	for _, c := range []struct {
		name      string
		prev, cur cpuTimes
		fresh     bool
	}{
		{"no previous reading", cpuTimes{}, cpuTimes{total: 11000, idle: 8250}, false},
		{"counters did not advance", cpuTimes{total: 11000, idle: 8250}, cpuTimes{total: 11000, idle: 8250}, true},
	} {
		got := cpuUsage(c.prev, c.cur, 2, 2, [3]float64{}, c.fresh, nil)
		if got.Measured || got.Status != StatusUnknown {
			t.Errorf("%s: measured=%v status=%q, want unmeasured/unknown", c.name, got.Measured, got.Status)
		}
	}

	if got := cpuUsage(cpuTimes{}, cpuTimes{}, 0, 0, [3]float64{}, false, os.ErrNotExist); got.Status != StatusUnknown {
		t.Errorf("unreadable /proc/stat: status %q, want unknown", got.Status)
	}
}

func TestReadCPUTopology(t *testing.T) {
	dir := t.TempDir()
	const cpuinfo = `processor	: 0
physical id	: 0
core id		: 0

processor	: 1
physical id	: 0
core id		: 0

processor	: 2
physical id	: 0
core id		: 1

processor	: 3
physical id	: 0
core id		: 1
`
	if err := os.WriteFile(filepath.Join(dir, "cpuinfo"), []byte(cpuinfo), 0o600); err != nil {
		t.Fatal(err)
	}
	cores, threads := readCPUTopology(dir)
	if cores != 2 || threads != 4 {
		t.Errorf("topology = %d cores / %d threads, want 2/4", cores, threads)
	}
}

func TestReadMemory(t *testing.T) {
	dir := fakeProc(t, "cpu 1 1 1 1 1 1 1 1\n", meminfoSample, "0.1 0.2 0.3 1/2 3\n", netdevSample)
	got := readMemory(dir)
	if !got.Measured {
		t.Fatalf("memory not measured: %+v", got)
	}
	if got.TotalBytes != 4*1024*1024*1024 {
		t.Errorf("total = %d bytes (%s), want 4 GiB", got.TotalBytes, got.TotalText())
	}
	// 4 GiB total, 2 GiB MemAvailable: half used (cache counted as available).
	if got.Percent() != 50 {
		t.Errorf("used = %.1f%% (%s), want 50%%", got.UsedPct, got.PctText())
	}
	if got.Status != StatusOK {
		t.Errorf("status = %q, want ok (%s)", got.Status, got.Detail)
	}
	if got.SwapUsedBytes != 512*1024*1024 {
		t.Errorf("swap used = %d bytes, want 512 MiB", got.SwapUsedBytes)
	}
}

func TestReadMemoryGrades(t *testing.T) {
	cases := []struct {
		name      string
		available string
		want      Status
	}{
		{"plenty free", "MemAvailable:    2097152 kB\n", StatusOK},
		{"little headroom", "MemAvailable:     209715 kB\n", StatusWarn},
		{"exhausted", "MemAvailable:      41943 kB\n", StatusError},
	}
	for _, c := range cases {
		dir := fakeProc(t, "cpu 1 1 1 1 1 1 1 1\n", "MemTotal: 4194304 kB\n"+c.available, "", netdevSample)
		if got := readMemory(dir); got.Status != c.want {
			t.Errorf("%s: status %q, want %q (%s)", c.name, got.Status, c.want, got.Detail)
		}
	}

	if got := readMemory(t.TempDir()); got.Status != StatusUnknown || got.Measured {
		t.Errorf("missing /proc/meminfo: status %q measured=%v", got.Status, got.Measured)
	}
}

func TestReadNetDevSkipsLoopbackAndHeaders(t *testing.T) {
	dir := fakeProc(t, "cpu 1 1 1 1 1 1 1 1\n", meminfoSample, "", netdevSample)
	got, err := readNetDev(dir)
	if err != nil {
		t.Fatalf("readNetDev: %v", err)
	}
	if _, ok := got["lo"]; ok {
		t.Error("loopback is counted as network traffic")
	}
	if len(got) != 2 {
		t.Fatalf("parsed %d interfaces, want eth0 and veth9: %+v", len(got), got)
	}
	if got["eth0"].rx != 1048576 || got["eth0"].tx != 524288 {
		t.Errorf("eth0 counters = %+v", got["eth0"])
	}
}

func TestNetworkUsageRates(t *testing.T) {
	prev := map[string]netCounters{"eth0": {rx: 1000, tx: 500}}
	cur := map[string]netCounters{
		"eth0":  {rx: 11000, tx: 5500},
		"veth9": {rx: 0, tx: 0}, // never carried anything: not shown
	}
	got := networkUsage(prev, cur, 5*time.Second, true, nil)
	if len(got.Interfaces) != 1 || got.Interfaces[0].Name != "eth0" {
		t.Fatalf("interfaces = %+v, want eth0 only", got.Interfaces)
	}
	// 10000 bytes in and 5000 out over five seconds.
	if got.RxRate != 2000 || got.TxRate != 1000 {
		t.Errorf("rates = %.0f in / %.0f out, want 2000/1000", got.RxRate, got.TxRate)
	}
	if got.InRateText() != "2.0 KiB/s" {
		t.Errorf("in rate text = %q", got.InRateText())
	}
	if got.Status != StatusOK {
		t.Errorf("status = %q, want ok — throughput is reported, not graded", got.Status)
	}
}

// A recreated container (or interface) resets the counters; the drop must not
// be reported as a huge negative or wrapped-around rate.
func TestNetworkUsageIgnoresCounterResets(t *testing.T) {
	prev := map[string]netCounters{"eth0": {rx: 1_000_000, tx: 900_000}}
	cur := map[string]netCounters{"eth0": {rx: 1000, tx: 900}}
	got := networkUsage(prev, cur, 5*time.Second, true, nil)
	if got.RxRate != 0 || got.TxRate != 0 {
		t.Errorf("rates after a counter reset = %.0f/%.0f, want 0/0", got.RxRate, got.TxRate)
	}
}

func TestNetworkUsageWithoutAPreviousReading(t *testing.T) {
	cur := map[string]netCounters{"eth0": {rx: 1000, tx: 500}}
	got := networkUsage(nil, cur, 0, false, nil)
	if got.Measured {
		t.Error("first reading reported as measured")
	}
	if len(got.Interfaces) != 1 || got.Interfaces[0].InText() != "1000 B" {
		t.Errorf("totals should be shown even before a rate exists: %+v", got.Interfaces)
	}
	if got.Detail == "" {
		t.Error("no explanation for the missing rates")
	}

	if bad := networkUsage(nil, nil, 0, false, os.ErrNotExist); bad.Status != StatusUnknown {
		t.Errorf("unreadable /proc/net/dev: status %q, want unknown", bad.Status)
	}
}

// End to end through the sampler: the first call baselines, the second reports.
func TestMachineSamplerNeedsTwoReadings(t *testing.T) {
	dir := fakeProc(t,
		"cpu  1000 0 500 8000 500 0 0 0\ncpu0 500 0 250 4000 250 0 0 0\ncpu1 500 0 250 4000 250 0 0 0\n",
		meminfoSample, "0.42 0.31 0.20 2/300 1234\n", netdevSample)
	m := &MachineSampler{procRoot: dir}

	first := m.Sample()
	if first.CPU.Measured || first.CPU.Status != StatusUnknown {
		t.Errorf("first sample reported a CPU figure: %+v", first.CPU)
	}
	if !first.Memory.Measured {
		t.Error("memory is a level, not a rate: it must be reported on the first sample")
	}
	if first.CPU.Cores != 2 || first.CPU.Threads != 2 {
		t.Errorf("cores/threads = %d/%d, want 2/2", first.CPU.Cores, first.CPU.Threads)
	}
	if first.CPU.Detail != "2 cores · 2 threads" {
		t.Errorf("detail = %q, want topology only", first.CPU.Detail)
	}
	if !first.CPU.HasLoad || first.CPU.Load[0] != 0.42 {
		t.Errorf("load average not read: %+v", first.CPU.Load)
	}
	// Unknown checks must not drag the card into a warning.
	if first.Status != StatusOK {
		t.Errorf("overall machine status = %q, want ok", first.Status)
	}

	// Second reading: 1000 more ticks, 750 of them idle → 25% busy.
	if err := os.WriteFile(filepath.Join(dir, "stat"),
		[]byte("cpu  1250 0 500 8750 500 0 0 0\ncpu0 625 0 250 4375 250 0 0 0\ncpu1 625 0 250 4375 250 0 0 0\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	second := m.Sample()
	if !second.CPU.Measured {
		t.Fatalf("second sample still unmeasured: %+v", second.CPU)
	}
	if second.CPU.Percent() != 25 {
		t.Errorf("busy = %s, want 25%%", second.CPU.BusyText())
	}
	if !second.Network.Measured || second.Window <= 0 {
		t.Errorf("network window not established: measured=%v window=%v", second.Network.Measured, second.Window)
	}
	if second.WindowText() == "" {
		t.Error("no sampling window to show on the card")
	}
}

// Outside Linux — a developer running the panel on their own machine — there is
// no /proc at all. Every metric must degrade to "unknown" rather than failing
// the status page.
func TestMachineSamplerWithoutProc(t *testing.T) {
	m := &MachineSampler{procRoot: filepath.Join(t.TempDir(), "absent")}
	got := m.Sample()
	if got.Status != StatusUnknown {
		t.Errorf("status = %q, want unknown", got.Status)
	}
	for name, st := range map[string]Status{"cpu": got.CPU.Status, "memory": got.Memory.Status, "network": got.Network.Status} {
		if st != StatusUnknown {
			t.Errorf("%s status = %q, want unknown", name, st)
		}
	}
}

func TestHumanBytes(t *testing.T) {
	cases := map[uint64]string{
		0:                      "0 B",
		999:                    "999 B",
		1024:                   "1.0 KiB",
		1536:                   "1.5 KiB",
		4 * 1024 * 1024 * 1024: "4.0 GiB",
	}
	for in, want := range cases {
		if got := humanBytes(in); got != want {
			t.Errorf("humanBytes(%d) = %q, want %q", in, got, want)
		}
	}
	if got := humanRate(0); got != "0 B/s" {
		t.Errorf("humanRate(0) = %q", got)
	}
}
