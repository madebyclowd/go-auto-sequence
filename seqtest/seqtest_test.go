package seqtest_test

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	sequence "github.com/madebyclowd/go-auto-sequence"
	"github.com/madebyclowd/go-auto-sequence/seqtest"
)

// recordingTB captures Fatalf instead of ending the test. It embeds testing.TB only to satisfy
// the interface; Helper and Fatalf are overridden.
type recordingTB struct {
	testing.TB
	msg string
}

type stop struct{}

func (r *recordingTB) Helper() {}
func (r *recordingTB) Fatalf(format string, args ...any) {
	r.msg = fmt.Sprintf(format, args...)
	panic(stop{})
}

// failure runs fn and returns the Fatalf message, or "" if fn finished without failing.
func failure(fn func(tb testing.TB)) (msg string) {
	r := &recordingTB{}
	defer func() {
		if p := recover(); p != nil {
			if _, ok := p.(stop); !ok {
				panic(p)
			}
			msg = r.msg
		}
	}()
	fn(r)
	return ""
}

func TestDefaultClockIsFixedAndMidPeriod(t *testing.T) {
	f := seqtest.New(t)
	now := f.Clock.Now()
	if !now.Equal(time.Date(2026, 6, 17, 12, 0, 0, 0, time.UTC)) {
		t.Fatalf("default time = %v", now)
	}
	time.Sleep(5 * time.Millisecond)
	if !f.Clock.Now().Equal(now) {
		t.Fatal("the clock must not move by itself")
	}
	// Away from every boundary: a day either side stays in the same month, quarter, ISO week and year.
	for _, d := range []time.Duration{-24 * time.Hour, 24 * time.Hour} {
		other := now.Add(d)
		for name, p := range map[string]sequence.Period{"monthly": sequence.Monthly, "quarterly": sequence.Quarterly, "weekly": sequence.Weekly, "yearly": sequence.Yearly} {
			if p(other) != p(now) {
				t.Errorf("%s boundary within a day of the default time", name)
			}
		}
	}
}

func TestFixtureEndToEndWithRollover(t *testing.T) {
	f := seqtest.New(t)
	ctx := context.Background()
	inv := f.Series("invoice", sequence.WithPeriod(sequence.Monthly))

	for want := int64(1); want <= 3; want++ {
		n, err := inv.Next(ctx)
		if err != nil || n.Seq != want || n.Period != "2026-06" {
			t.Fatalf("got %+v, %v", n, err)
		}
	}
	f.Clock.Advance(32 * 24 * time.Hour)
	n, err := inv.Next(ctx)
	if err != nil || n.Seq != 1 || n.Period != "2026-07" {
		t.Fatalf("after the month rolled over: %+v, %v", n, err)
	}
	if f.Store.Peek(sequence.Key{Name: "invoice"}, "2026-06") != 3 {
		t.Fatal("Store must expose the raw counters")
	}
	f.Clock.Set(time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC))
	if n, _ := inv.Next(ctx); n.Period != "2030-01" {
		t.Fatalf("Set: %+v", n)
	}
}

func TestAtOption(t *testing.T) {
	at := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	f := seqtest.New(t, seqtest.At(at))
	n, err := f.Series("x", sequence.WithPeriod(sequence.Yearly)).Next(context.Background())
	if err != nil || n.Period != "2027" {
		t.Fatalf("got %+v, %v", n, err)
	}
}

func TestWithSequencerOptionsForwards(t *testing.T) {
	jakarta := time.FixedZone("WIB", 7*3600)
	var got []sequence.Exhaustion
	f := seqtest.New(t,
		seqtest.At(time.Date(2026, 12, 31, 17, 30, 0, 0, time.UTC)),
		seqtest.WithSequencerOptions(
			sequence.WithLocation(jakarta),
			sequence.WithExhaustionHandler(func(_ context.Context, e sequence.Exhaustion) { got = append(got, e) }),
		))
	s := f.Series("x", sequence.WithPeriod(sequence.Yearly), sequence.WithMax(2, 50))
	n, err := s.Next(context.Background())
	if err != nil || n.Period != "2027" {
		t.Fatalf("WithLocation must apply: %+v, %v", n, err)
	}
	if len(got) != 1 {
		t.Fatalf("WithExhaustionHandler must apply: %d calls", len(got))
	}
}

func TestUserClockOptionReplacesTheFakeClock(t *testing.T) {
	fixed := time.Date(2031, 3, 3, 0, 0, 0, 0, time.UTC)
	f := seqtest.New(t, seqtest.WithSequencerOptions(sequence.WithClock(func() time.Time { return fixed })))
	n, _ := f.Series("x", sequence.WithPeriod(sequence.Yearly)).Next(context.Background())
	if n.Period != "2031" {
		t.Fatalf("a caller-supplied clock wins, got %+v", n)
	}
}

func TestInvalidInputFailsTheTest(t *testing.T) {
	if msg := failure(func(tb testing.TB) { seqtest.New(tb, seqtest.At(time.Time{})) }); !strings.Contains(msg, "zero time") {
		t.Errorf("zero At: %q", msg)
	}
	if msg := failure(func(tb testing.TB) {
		seqtest.New(tb, seqtest.WithSequencerOptions(sequence.WithLocation(nil)))
	}); !strings.Contains(msg, "location") {
		t.Errorf("bad sequencer option: %q", msg)
	}
	if msg := failure(func(tb testing.TB) { seqtest.New(tb).Series("", sequence.WithStart(1)) }); !strings.Contains(msg, "series") {
		t.Errorf("bad series: %q", msg)
	}
	if msg := failure(func(tb testing.TB) { seqtest.New(tb).Series("ok") }); msg != "" {
		t.Errorf("a valid series must not fail: %q", msg)
	}
}

func TestClockIsSafeForConcurrentUse(t *testing.T) {
	f := seqtest.New(t)
	s := f.Series("x", sequence.WithPeriod(sequence.Daily))
	var wg sync.WaitGroup
	for g := range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 100 {
				if g%4 == 0 {
					f.Clock.Advance(time.Minute)
				} else if _, err := s.Next(context.Background()); err != nil {
					t.Error(err)
					return
				}
			}
		}()
	}
	wg.Wait()
}

// ExampleNew shows a period rollover test. It is compile-checked only: a real test receives its
// *testing.T from the test function.
func ExampleNew() {
	var t *testing.T // provided by the test function in real use
	f := seqtest.New(t)
	inv := f.Series("invoice", sequence.WithPeriod(sequence.Monthly))

	first, _ := inv.Next(context.Background())
	f.Clock.Advance(32 * 24 * time.Hour) // the month rolls over
	second, _ := inv.Next(context.Background())
	fmt.Println(first.Period, second.Period, second.Seq)
}
