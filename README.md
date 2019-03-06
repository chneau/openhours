# openhours

[![Build Status](https://travis-ci.org/chneau/openhours.svg?branch=master)](https://travis-ci.org/chneau/openhours)

A compromise of complexity of the ["opening_hours"](https://wiki.openstreetmap.org/wiki/Key:opening_hours).  
Only the `day-day time-time" will work for now.  

## Online tools

<https://openingh.openstreetmap.de/evaluation_tool/?setLng=en>

## Example

```go
oh := openhours.New("Mo-Fr 09:00-17:00")
t := time.Date(2019, 3, 6, 10, 0, 0, 0, time.Now().Location())
// oh.Location = t.Location() //default to system but can be changed this way

fmt.Println("t =", t)

isOpen := oh.Match(t)
fmt.Println("Is it open at this date?", isOpen)

_, duration := oh.NextDur(t)
fmt.Println("For how long?", duration)

_, date := oh.NextDate(t)
fmt.Println("When will it close?", date)

fmt.Println(" +++++++++++++++++++++ ")

t = time.Date(2019, 3, 6, 18, 0, 0, 0, time.Now().Location())

fmt.Println("t =", t)

isOpen, date = oh.NextDate(t)
fmt.Println("Is it open?", isOpen, "Ok, when will it open then ?", date)

/* output
t = 2019-03-06 10:00:00 +0000 GMT
Is it open at this date? true
For how long? 7h0m0s
When will it close? 2019-03-06 17:00:00 +0000 GMT
 +++++++++++++++++++++
t = 2019-03-06 18:00:00 +0000 GMT
Is it open? false Ok, when will it open then ? 2019-03-07 09:00:00 +0000 GMT
*/
```
