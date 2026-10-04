package sequence_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	sequence "github.com/madebyclowd/go-auto-sequence"
	"github.com/madebyclowd/go-auto-sequence/memstore"
)

type recorder struct {
	mu  sync.Mutex
	got []sequence.Exhaustion
}

func (r *recorder) handle(_ context.Context, e sequence.Exhaustion) {
	r.mu.Lock()
	r.got = append(r.got, e)
	r.mu.Unlock()
}

func newMaxSeq(t *testing.T, rec *recorder) *sequence.Sequencer {
	t.Helper()
	var opts []sequence.Option
	if rec != nil {
		opts = append(opts, sequence.WithExhaustionHandler(rec.handle))
	}
	s, err := sequence.New(memstore.New(), opts...)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestWithMaxHardStop(t *testing.T) {
	seq := newMaxSeq(t, nil)
	s := mustSeries(t, seq, "x", sequence.WithStart(8), sequence.WithMax(10, 90))
	for _, want := range []int64{8, 9, 10} {
		if got := next(t, s).Seq; got != want {
			t.Fatalf("got %d, want %d", got, want)
		}
	}
	_, err := s.Next(context.Background())
	var ee *sequence.ExhaustedError
	if !errors.Is(err, sequence.ErrExhausted) || !errors.As(err, &ee) || ee.Max != 10 || ee.Seq != 11 || ee.Key.Name != "x" {
		t.Fatalf("err = %v (%+v)", err, ee)
	}
	// Further calls keep failing, and Current reports the real last value, not the attempts.
	if _, err := s.Next(context.Background()); !errors.Is(err, sequence.ErrExhausted) {
		t.Fatalf("err = %v", err)
	}
	if last, issued, _ := s.Current(context.Background()); !issued || last != 10 {
		t.Fatalf("Current = (%d, %v), want (10, true)", last, issued)
	}
}

func TestWithMaxConfigErrors(t *testing.T) {
	seq := newMaxSeq(t, nil)
	for name, opts := range map[string][]sequence.SeriesOption{
		"max below start":  {sequence.WithStart(10), sequence.WithMax(9, 90)},
		"percent zero":     {sequence.WithMax(10, 0)},
		"percent over 100": {sequence.WithMax(10, 101)},
		"percent negative": {sequence.WithMax(10, -5)},
	} {
		if _, err := seq.Series("x", opts...); !errors.Is(err, sequence.ErrInvalidConfig) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	if _, err := seq.Series("ok", sequence.WithStart(5), sequence.WithMax(5, 100)); err != nil {
		t.Fatalf("max == start is valid: %v", err)
	}
}

func TestThresholdMath(t *testing.T) {
	cases := []struct {
		start, max int64
		pct        int
		want       int64
	}{
		{1, 100, 90, 90},
		{1, 10, 85, 9}, // ceil(8.5)
		{1, 10, 100, 10},
		{1, 10, 1, 1},
		{1000, 1099, 50, 1049},
		{0, 99, 90, 89},
		{1, 3, 50, 2},
	}
	for _, c := range cases {
		rec := &recorder{}
		seq := newMaxSeq(t, rec)
		s := mustSeries(t, seq, "x", sequence.WithStart(c.start), sequence.WithMax(c.max, c.pct))
		for i := c.start; i <= c.max; i++ {
			next(t, s)
		}
		if len(rec.got) != 1 || rec.got[0].Threshold != c.want || rec.got[0].Seq != c.want {
			t.Errorf("start=%d max=%d pct=%d: got %+v, want threshold and seq %d", c.start, c.max, c.pct, rec.got, c.want)
		}
	}
}

func TestHandlerFiresOnceAtTheCrossing(t *testing.T) {
	rec := &recorder{}
	seq := newMaxSeq(t, rec)
	s := mustSeries(t, seq, "inv", sequence.WithMax(10, 80), sequence.WithPeriod(sequence.Yearly))
	for i := 1; i <= 7; i++ {
		next(t, s)
	}
	if len(rec.got) != 0 {
		t.Fatalf("fired early: %+v", rec.got)
	}
	next(t, s) // 8 = threshold
	if len(rec.got) != 1 {
		t.Fatalf("handler calls = %d, want 1", len(rec.got))
	}
	e := rec.got[0]
	if e.Key.Name != "inv" || e.Max != 10 || e.Threshold != 8 || e.Seq != 8 || e.Period == "" {
		t.Fatalf("got %+v", e)
	}
	next(t, s)
	next(t, s)
	if _, err := s.Next(context.Background()); !errors.Is(err, sequence.ErrExhausted) {
		t.Fatal(err)
	}
	if len(rec.got) != 1 {
		t.Fatalf("handler fired again: %d calls", len(rec.got))
	}
}

func TestHandlerFiresOnceUnderConcurrency(t *testing.T) {
	var calls atomic.Int64
	seq, _ := sequence.New(memstore.New(), sequence.WithExhaustionHandler(func(context.Context, sequence.Exhaustion) { calls.Add(1) }))
	s := mustSeries(t, seq, "x", sequence.WithMax(5000, 50))
	var wg sync.WaitGroup
	for range 200 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 30 { // 6000 attempts for 5000 numbers
				_, _ = s.Next(context.Background())
			}
		}()
	}
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatalf("handler calls = %d, want exactly 1", calls.Load())
	}
}

func TestReserveWithMaxAndCrossing(t *testing.T) {
	rec := &recorder{}
	seq := newMaxSeq(t, rec)
	s := mustSeries(t, seq, "x", sequence.WithMax(100, 50))
	if _, err := s.Reserve(context.Background(), 40); err != nil {
		t.Fatal(err)
	}
	if len(rec.got) != 0 {
		t.Fatal("below the threshold")
	}
	ns, err := s.Reserve(context.Background(), 20) // 41..60 crosses 50
	if err != nil || len(ns) != 20 {
		t.Fatal(err)
	}
	if len(rec.got) != 1 || rec.got[0].Seq != 50 || rec.got[0].Threshold != 50 {
		t.Fatalf("got %+v", rec.got)
	}
	if _, err := s.Reserve(context.Background(), 50); !errors.Is(err, sequence.ErrExhausted) { // 61..110 passes max
		t.Fatalf("err = %v", err)
	}
	if len(rec.got) != 1 {
		t.Fatal("a failed reserve issues nothing and must not notify")
	}
	// A reservation that starts above the threshold does not re-notify.
	s2 := mustSeries(t, seq, "y", sequence.WithMax(100, 10))
	if _, err := s2.Reserve(context.Background(), 20); err != nil {
		t.Fatal(err)
	}
	n := len(rec.got)
	if _, err := s2.Reserve(context.Background(), 20); err != nil {
		t.Fatal(err)
	}
	if len(rec.got) != n {
		t.Fatal("second reservation is past the threshold already")
	}
	if ns, _ := s2.Reserve(context.Background(), 5); len(ns) != 5 {
		t.Fatal("reserve exactly up to max must work")
	}
}

func TestResetReArmsTheHandler(t *testing.T) {
	rec := &recorder{}
	seq := newMaxSeq(t, rec)
	s := mustSeries(t, seq, "x", sequence.WithMax(10, 50))
	for range 6 {
		next(t, s)
	}
	if len(rec.got) != 1 {
		t.Fatal(len(rec.got))
	}
	if err := s.Reset(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	for range 6 {
		next(t, s)
	}
	if len(rec.got) != 2 {
		t.Fatalf("handler calls = %d, want 2 after a reset", len(rec.got))
	}
}

func TestResetLimitWithMax(t *testing.T) {
	seq := newMaxSeq(t, nil)
	s := mustSeries(t, seq, "x", sequence.WithMax(10, 90))
	ctx := context.Background()
	if err := s.Reset(ctx, 11); err != nil { // max+1 marks the series full
		t.Fatal(err)
	}
	if _, err := s.Next(ctx); !errors.Is(err, sequence.ErrExhausted) {
		t.Fatalf("err = %v", err)
	}
	if err := s.Reset(ctx, 12); !errors.Is(err, sequence.ErrInvalidConfig) {
		t.Fatalf("err = %v", err)
	}
}

func TestHandlerNotCalledWhenFormattingFails(t *testing.T) {
	rec := &recorder{}
	seq := newMaxSeq(t, rec)
	s := mustSeries(t, seq, "x", sequence.WithMax(4, 25), sequence.WithFormat(sequence.FormatFunc(func(sequence.Parts) (string, error) {
		return "", errors.New("boom")
	})))
	if _, err := s.Next(context.Background()); err == nil {
		t.Fatal("want the formatter error")
	}
	if len(rec.got) != 0 {
		t.Fatal("no number was issued, so no notification")
	}
}

func TestCrossingIsLoggedWhenNoHandler(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, nil))
	seq, _ := sequence.New(memstore.New(), sequence.WithLogger(log))
	s := mustSeries(t, seq, "inv", sequence.WithMax(10, 50))
	for range 4 {
		next(t, s)
	}
	if buf.Len() != 0 {
		t.Fatalf("logged early: %s", buf.String())
	}
	next(t, s)
	out := buf.String()
	if !strings.Contains(out, "level=WARN") || !strings.Contains(out, "name=inv") || !strings.Contains(out, "threshold=5") {
		t.Fatalf("log output: %q", out)
	}
	// With a handler registered, the library does not also log.
	buf.Reset()
	seq2, _ := sequence.New(memstore.New(), sequence.WithLogger(log), sequence.WithExhaustionHandler(func(context.Context, sequence.Exhaustion) {}))
	s2 := mustSeries(t, seq2, "inv", sequence.WithMax(10, 10))
	next(t, s2)
	if buf.Len() != 0 {
		t.Fatalf("handler present, but logged: %s", buf.String())
	}
}

func TestNoMaxMeansNoHandlerCalls(t *testing.T) {
	rec := &recorder{}
	seq := newMaxSeq(t, rec)
	s := mustSeries(t, seq, "x")
	for range 50 {
		next(t, s)
	}
	if len(rec.got) != 0 {
		t.Fatal("no WithMax, no notification")
	}
}
