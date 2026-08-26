# openhours

A high-performance Go parser and evaluator for OpenStreetMap [`opening_hours`](https://wiki.openstreetmap.org/wiki/Key:opening_hours) specifications.

## Features

- **Interval Math**: Bakes rules into disjoint week-minute time windows without bitset scanning.
- **Fast Evaluation**: $O(\log N)$ point-in-time checks via binary search.
- **Instance Interning**: Caches and shares identical schedules concurrently to minimize memory allocations.
- **Overnight Shifts**: Supports shifts spanning across midnight (e.g. `Mo 22:00-04:00`, `Su 22:00-04:00`).
- **Overrides & Exclusions**: Supports `off` / `closed` rules overriding previous rules (e.g. `Mo-Su 00:00-24:00; Tu 12:00-13:00 off`).
- **Shortcuts & Open-Ended Intervals**: Supports `24/7`, open-ended (`Mo 10:00+`), day-only rules (`Mo-Fr`), and time-only rules (`10:00-12:00`).
- **Duration Queries**: Find when an opening window long enough for a task of duration $D$ is available (`GetTimeToOpenForDuration` / `When`).
- **JSON Support**: Native `json.Marshaler` and `json.Unmarshaler` implementations.

## Online Tools

<https://openingh.openstreetmap.de/evaluation_tool/?setLng=en>

## Examples

### Modern API (`OpeningHours`)

```go
package main

import (
	"fmt"
	"time"

	"github.com/chneau/openhours/v2"
)

func main() {
	// Parse an OSM opening_hours expression
	oh := openhours.Parse("Mo-Fr 08:00-12:00, 13:00-17:00; Sa 08:00-12:00")

	now := time.Date(2026, 5, 18, 10, 0, 0, 0, time.UTC) // Monday 10:00 AM

	// Check if open
	fmt.Println("Is open:", oh.IsOpen(now)) // true

	// Get current shift end
	if end := oh.GetCurrentShiftEnd(now); end != nil {
		fmt.Println("Current shift ends at:", *end) // 2026-05-18 12:00:00
	}

	// Time to next open
	wait := oh.GetTimeToOpen(time.Date(2026, 5, 18, 12, 30, 0, 0, time.UTC))
	fmt.Println("Opens in:", *wait) // 30m

	// Find wait time for continuous duration (e.g. 3-hour job)
	waitDur := oh.GetTimeToOpenForDuration(time.Date(2026, 5, 18, 11, 0, 0, 0, time.UTC), 3*time.Hour)
	fmt.Println("Wait for 3h slot:", *waitDur) // 2h (opens at 13:00)
}
```

## Development & Quality Commands

Useful commands for local development, testing, benchmarking, and linting:

### Testing & Coverage

```bash
# Run all tests
go test ./...

# Run tests with verbose output and race detector
go test -v -race ./...

# Run tests with statement coverage report
go test -cover ./...
```

### Benchmarks

```bash
# Run benchmarks with memory allocation metrics
go test -benchmem -bench=. ./...

# Run specific benchmark function (e.g. IsOpen)
go test -benchmem -bench=BenchmarkIsOpen ./...
```

### Linting & Code Quality

```bash
# Run golangci-lint
golangci-lint run ./...

# Automatically fix supported linter issues
golangci-lint run --fix ./...

# Run gocritic checks
gocritic check ./...

# Automatically fix gocritic issues (e.g. switchTrue / tagged switches)
gocritic check -enable=switchTrue -fix ./...
```


