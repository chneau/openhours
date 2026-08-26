# openhours

A high-performance, zero-allocation Go parser and interval-math evaluator for OpenStreetMap [`opening_hours`](https://wiki.openstreetmap.org/wiki/Key:opening_hours) specifications.

[![Go Reference](https://pkg.go.dev/badge/github.com/chneau/openhours/v2.svg)](https://pkg.go.dev/github.com/chneau/openhours/v2)
[![Go Report Card](https://goreportcard.com/badge/github.com/chneau/openhours/v2)](https://goreportcard.com/report/github.com/chneau/openhours/v2)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

---

## ⚡ Features & Performance

- **$O(1)$ Hardware-Accelerated Bitmask Table**: Evaluates `IsOpen` in **~4.5 nanoseconds** (>220 Million ops/sec) via embedded `[158]uint64` scalar bit tests.
- **Zero-Allocation Interval Math**: Zero heap allocations on query paths (`IsOpen`, `TimeToOpen`, `TimeToOpenForDuration`, `WhenTime`, `NextDur`, `NextDate`).
- **Thread-Safe & Lock-Free Interning**: Automatic lock-free interning and caching of parsed expressions.
- **Overnight Shifts**: Full support for shifts spanning midnight (e.g. `Mo 22:00-04:00`, `Su 22:00-04:00`).
- **Overrides & Exclusions**: Handles `off` / `closed` rules overriding previous rules (e.g. `Mo-Su 00:00-24:00; Tu 12:00-13:00 off`).
- **Duration Availability**: Find wait times for contiguous tasks of duration $D$ (`GetTimeToOpenForDuration` / `When`).
- **Standard JSON Support**: Native `json.Marshaler` and `json.Unmarshaler` implementations.
- **Reflection-free JSON Decode**: A dependency-free `DecodeJSON([]byte)` entry point that decodes a JSON string in **~26 ns with zero allocations** (`*OpeningHours` is the shared interned instance) — no runtime reflection, no third-party modules.

---

## 🧠 Optimizations & Engineering Architecture

The Go implementation leverages specialized low-level and compiler-aware optimizations to achieve sub-10ns evaluations and zero GC overhead:

1. **Dual State Representation**:
   - **Disjoint Interval Table (`[]TimeWindow`)**: Minute intervals within the week `[0, 10080)` stored as `{Start, End int}`. Intervals are sorted and disjoint.
   - **Hardware Bitmask Table (`[158]uint64`)**: A 10,080-bit packed bitmask where bit $i$ represents minute $i$ of the week. Point-in-time checks (`IsOpen`) execute in $O(1)$ via scalar bit tests: `(bitmask[weekMin >> 6] & (1 << (weekMin & 63))) != 0`.

2. **$O(\log N)$ Binary Search with Small-Window Unrolling**:
   - Interval-based lookups (`GetTimeToOpen`, `When`, `NextDur`) utilize binary search over the sorted `windows` array. For short schedules ($N \le 3$), branches are unrolled directly.
   - Includes compiler Bounds Check Elimination (BCE) hints (`_ = windows[n-1]`) to eliminate slice bounds checking inside hot search loops.

3. **Two-Tier Lock-Free Caching Hierarchy**:
   - **L1 Atomic Slot**: An `atomic.Pointer[parseSlot]` holds the most recently accessed expression. Consecutive lookups on the same expression bypass lock contention and hash table lookups completely.
   - **L2 Concurrent Intern Pool**: Backed by `sync.RWMutex` and `map[string]*OpeningHours` for deduplicating parsed expressions globally.

4. **Reflection-Free JSON Fast Path**:
   - `DecodeJSON([]byte)` directly scans quotes, validates escapes, and resolves directly to the interned `*OpeningHours` instance via `stringEqualsBytes` and `unsafe.StringData`, eliminating runtime reflection and allocations.

5. **Zero-Allocation Stack Parsing**:
   - Parsing uses stack-allocated buffers (`[8]openingRule`, `[4]timeRange`) and ASCII byte scanning, completely avoiding heap allocations during rule tokenization.

---

## 🚀 Quick Start

### Installation

```bash
go get github.com/chneau/openhours/v2
```

### Usage Example

```go
package main

import (
	"fmt"
	"time"

	"github.com/chneau/openhours/v2"
)

func main() {
	// 1. Parse an OSM opening_hours string
	oh := openhours.Parse("Mo-Fr 08:00-12:00, 13:00-17:00; Sa 08:00-12:00")

	monday10am := time.Date(2026, 5, 18, 10, 0, 0, 0, time.UTC)

	// 2. Fast point-in-time check (4.5 ns/op)
	isOpen := oh.IsOpen(monday10am) // true
	fmt.Println("Is open:", isOpen)

	// 3. Current shift end
	if shiftEnd := oh.GetCurrentShiftEnd(monday10am); shiftEnd != nil {
		fmt.Println("Current shift ends at:", *shiftEnd) // 2026-05-18 12:00:00 UTC
	}

	// 4. Time to next open
	tuesdayLunch := time.Date(2026, 5, 19, 12, 30, 0, 0, time.UTC)
	timeToOpen := oh.GetTimeToOpen(tuesdayLunch) // 30m (opens at 13:00)
	fmt.Println("Opens in:", *timeToOpen)

	// 5. Find when a 3-hour job can be serviced
	waitFor3h := oh.GetTimeToOpenForDuration(tuesdayLunch, 3*time.Hour)
	whenCanStart := oh.When(tuesdayLunch, 3*time.Hour) // 2026-05-19 13:00:00 UTC
	fmt.Println("Wait for 3h slot:", *waitFor3h, "Starts at:", *whenCanStart)

	// 6. Next state transitions
	isOpenNow, durationRemaining := oh.NextDur(monday10am)
	_, nextTransitionDate := oh.NextDate(monday10am) // 2026-05-18 12:00:00 UTC
	fmt.Println("Open now:", isOpenNow, "Remaining:", durationRemaining, "Next date:", nextTransitionDate)
}
```

### Fast, Reflection-free JSON Decode

`OpeningHours` serializes to a JSON string of the expression. Use `DecodeJSON` for a dependency-free, reflection-free decode that returns the shared interned `*OpeningHours` (~26 ns, zero allocations):

```go
oh, err := openhours.DecodeJSON([]byte(`"Mo-Fr 08:00-12:00, 13:00-17:00"`))
if err != nil {
    log.Fatal(err)
}
fmt.Println(oh.IsOpen(monday10am))
```

---

## 📊 Benchmark Suite (Go on AMD Ryzen 9)

| # | Workload | Calls | Latency / Op | Throughput |
| :--- | :--- | :--- | :--- | :--- |
| **1** | **`IsOpen` (Pure call)** | 1,000,000 | **4.5 ns** | 220,000,000 ops/sec |
| **2** | **`TimeToOpen` (Zero-alloc)** | 10,000 | **9.5 ns** | 105,000,000 ops/sec |
| **3** | **`Parse` (Interned / Cached)** | 1,000 | **14.5 ns** | 69,000,000 ops/sec |
| **4** | **`When`** | 10,000 | **23.0 ns** | 43,000,000 ops/sec |
| **5** | **`NextDur`** | 10,000 | **25.5 ns** | 39,000,000 ops/sec |
| **6** | **`TimeToOpenForDuration` (Zero-alloc)** | 10,000 | **27.8 ns** | 36,000,000 ops/sec |
| **7** | **`GetTimeToOpen`** | 10,000 | **35.3 ns** | 28,000,000 ops/sec |
| **8** | **`NextDate`** | 10,000 | **39.8 ns** | 25,000,000 ops/sec |
| **9** | **`GetTimeToOpenForDuration`** | 10,000 | **47.5 ns** | 21,000,000 ops/sec |
| **10** | **`Parse` (Uncached)** | 1,000 | **345.5 ns** | 2,900,000 ops/sec |

---

## 🛠️ Development & Quality Commands

```bash
# Run all unit tests
go test ./...

# Run tests with race detector and verbose output
go test -v -race ./...

# Run tests with statement coverage report
go test -cover ./...

# Run benchmarks with memory allocation metrics
go test -benchmem -bench=. ./...

# Run linter
golangci-lint run ./...
```

---

## 📄 License

MIT License. Copyright (c) 2026 chneau.
