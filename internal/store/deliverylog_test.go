package store

import (
	"testing"
	"time"
)

// The counters add up per UTC day, the stuck level is overwritten rather than
// added, nothing is written for nothing, and a year is kept.
func TestDeliveryLogDays(t *testing.T) {
	st := openTestStore(t)
	day1 := time.Date(2026, 10, 14, 23, 59, 0, 0, time.UTC)
	day2 := day1.Add(2 * time.Minute)

	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(st.AddReconcileClosed(day1, 2))
	must(st.AddReconcileClosed(day1, 3))
	must(st.AddReconcileClosed(day1, 0))
	must(st.AddReconcileMissed(day1, 2))
	must(st.AddTailerBlind(day1, 1500*time.Millisecond))
	must(st.AddTailerBlind(day1, 900*time.Millisecond)) // under a second: nothing
	must(st.SetQueuedOverLifetime(day1, 4))
	must(st.SetQueuedOverLifetime(day1, 1))
	must(st.AddTailerBlind(day2, time.Minute))

	days, err := st.DeliveryLogDays()
	must(err)
	want := []DeliveryLogDay{
		{Day: "2026-10-14", Closed: 5, Missed: 2, BlindSeconds: 1, Stuck: 1},
		{Day: "2026-10-15", BlindSeconds: 60},
	}
	if len(days) != len(want) {
		t.Fatalf("days = %+v, want %+v", days, want)
	}
	for i := range want {
		if days[i] != want[i] {
			t.Errorf("day %d = %+v, want %+v", i, days[i], want[i])
		}
	}

	must(st.PruneDeliveryLogDays(day2.AddDate(1, 0, 1)))
	days, err = st.DeliveryLogDays()
	must(err)
	if len(days) != 1 || days[0].Day != "2026-10-15" {
		t.Fatalf("after pruning a year and a day later: %+v, want 2026-10-15 alone", days)
	}
}

// Only the counters may be added to by name.
func TestAddDeliveryLogDayRefusesAnotherColumn(t *testing.T) {
	st := openTestStore(t)
	if err := st.addDeliveryLogDay(time.Now(), "day", 1); err == nil {
		t.Fatal("addDeliveryLogDay took the column day")
	}
}

func TestCountQueuedOlderThan(t *testing.T) {
	st := openTestStore(t)
	for _, e := range []SendLogEntry{{QueueID: "Q1", To: "a@example.net"}, {QueueID: "Q2", To: "b@example.net"}} {
		if err := st.InsertQueued(e); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := st.UpdateStatus("Q2", "b@example.net", StatusSent); err != nil {
		t.Fatal(err)
	}
	if n, err := st.CountQueuedOlderThan(time.Now().Add(time.Hour)); err != nil || n != 1 {
		t.Fatalf("queued rows before an hour from now = %d, %v; want 1", n, err)
	}
	if n, err := st.CountQueuedOlderThan(time.Now().Add(-time.Hour)); err != nil || n != 0 {
		t.Fatalf("queued rows before an hour ago = %d, %v; want 0", n, err)
	}
}

// Statistics start no earlier than the entity: what came before is a
// predecessor's of the same name (review of audit stage 2).
func TestStatsWindowStartsAtTheEntity(t *testing.T) {
	created := time.Now().UTC().Add(-48 * time.Hour)
	since, _, _ := StatsWindow(90, created)
	if !since.Equal(created) {
		t.Fatalf("since = %s, want the entity's creation %s", since, created)
	}
	old := time.Now().UTC().AddDate(-1, 0, 0)
	since, _, _ = StatsWindow(90, old)
	if since.Before(time.Now().UTC().AddDate(0, 0, -31)) {
		t.Fatalf("since = %s, want within the 30-day window", since)
	}
}
