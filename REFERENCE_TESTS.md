# Reference tests from opening_hours.js

`reference_test.go` ports the point-in-time open/closed assertions from the
original [opening_hours.js test suite](https://github.com/opening-hours/opening_hours.js/blob/main/test/test.js).

The reference suite is made up of `test.addTest(...)` (336), `addShouldFail`
(30), `addShouldWarn` (4), `addStructuredWarnings` (35), `addCompMatchingRule`
(5), `addPrettifyValue` (19), `addEqualTo` (6), and `addNextChangeTest` (3),
plus 14 `test/unit` cases. Under the "don't grow the public API" constraint,
only the interval-based `addTest` cases can be ported here, and only the ones
this parser already evaluates identically.

## What was ported

For every `test.addTest(...)` block in the reference suite whose expression
variants this parser accepts **and** evaluates to the **same open intervals**,
we assert `IsOpen` at:

- every expected-interval boundary (`start-1min`, `start`, `start+1min`,
  midpoint, `end-1min`, `end`),
- the query-window start, and a coarse daily grid inside the window.

That yields **48 portable reference cases** (this repo covers them all). They
exercise: simple + comma/`;`-separated time ranges, `off`/`closed` overrides,
always-closed schedules, overnight spans (e.g. `22:00-02:00`), day-selector
lists and wrap-around ranges, "no time means all day" selectors, 24h schedules
and `24/7`.

## What was NOT ported (outside this parser's grammar / the public API)

Porting is restricted to expressions that already parse and behave identically.
The following reference-suite features are **not** portable because they need
grammar/behaviours this library does not expose, so they are skipped:

- **open-end / variable end** (`17:00+`, `sunset`, `dawn`, `14:00-sunset+`, …)
- **am/pm, dot and unicode time-separator tolerance**
  (`1pm-8pm`, `10.00-12.00`, `12h ß`-style dash variants, short `10-12`) that this
  parser does not accept
- **"additional"/"exception" rules that re-select the same weekday via `,`/`;`**
  (e.g. `Mo-Fr 10:00-16:00, We 12:00-18:00`) — this parser only unions
  once-per-day ranges
- **holidays** (`PH`, `SH`, `e.g. Germany`) and **region/*Nominatim*** data
- **variable days** (month/week/day numbers, `Mo[1]`, `Jul-Aug`, `2025 Jul 27`)
- **comments** (`"..."`), `prettifyValue`, warnings, `isWeekStable`
- the `addShouldFail`/`addShouldWarn`/`addStructuredWarnings`/
  `addPrettifyValue`/`addEqualTo`/`addCompMatchingRule`/`addNextChangeTest`
  cases: they rely on APIs (warnings, prettify, equivalence, next-change
  semantics) that are not part of this library's API.

If a future version of this parser grows grammar/APIs for any of the above, the
corresponding reference cases can be added.

## Per-language differences

The four sibling ports (`openhours`, `openhours-cs`, `openhours-java`,
`openhours-rs`) are independent parsers, so the ported case set can differ where
a language's parser does not implement the same sub-grammar. See each repo's own
reference test file header for the exact exclusions.
