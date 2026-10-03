package sequence_test

import (
	"context"
	"fmt"
	"time"

	sequence "github.com/madebyclowd/go-auto-sequence"
	"github.com/madebyclowd/go-auto-sequence/memstore"
)

func Example() {
	clock := func() time.Time { return time.Date(2026, 10, 3, 9, 0, 0, 0, time.UTC) }
	seq, _ := sequence.New(memstore.New(), sequence.WithClock(clock))

	format, _ := sequence.ParseFormat("INV-{YYYY}-{seq:5}")
	invoice, _ := seq.Series("invoice", sequence.WithFormat(format), sequence.WithPeriod(sequence.Yearly))

	n, _ := invoice.Next(context.Background())
	fmt.Println(n)
	// Output: INV-2026-00001
}

func ExampleWithScope() {
	seq, _ := sequence.New(memstore.New())
	order, _ := seq.Series("order", sequence.RequireScope())

	a, _ := order.Next(context.Background(), sequence.WithScope("tenant-a"))
	b, _ := order.Next(context.Background(), sequence.WithScope("tenant-b"))
	_, err := order.Next(context.Background())
	fmt.Println(a, b, err)
	// Output: 1 1 sequence: scope required for this series: series "order"
}
