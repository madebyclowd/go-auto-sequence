# go-auto-sequence

Framework-agnostic sequential document numbers for Go, like `INV-2026-00042` or
`ORD-999`. Each new number is larger than the last, and two callers never get the
same one, even under heavy concurrency.

> **Status: v0, pre-release.** Nothing is published yet and the API may change in
> minor releases until v1.0.0.

```go
import "github.com/madebyclowd/go-auto-sequence" // package name: sequence
```

The repository is `go-auto-sequence`; the package is `sequence`, so you write
`sequence.New(...)`.

## License

MIT. See [LICENSE](LICENSE).
