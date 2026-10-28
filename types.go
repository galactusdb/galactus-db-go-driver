// Package galactus is a standard-library-only Bolt 5.4 driver with a Bolt 4.4
// fallback.
package galactus

import (
	"strconv"
	"time"
)

type Structure struct {
	Tag    byte
	Fields []any
}

// Node is a returned graph node. ElementID is the Bolt 5 element id; on a
// Bolt 4.4 connection it holds the decimal text of ID.
type Node struct {
	ID         int64
	Labels     []any
	Properties map[string]any
	ElementID  string
}

// Relationship is a returned graph relationship. The element ids are filled
// from the integer ids on Bolt 4.4.
type Relationship struct {
	ID, StartID, EndID int64
	Type               string
	Properties         map[string]any
	ElementID          string
	StartElementID     string
	EndElementID       string
}

// UnboundRelationship is a relationship inside a Path.
type UnboundRelationship struct {
	ID         int64
	Type       string
	Properties map[string]any
	ElementID  string
}
type Path struct{ Nodes, Relationships, Sequence []any }
type Date struct{ Days int64 }
type LocalTime struct{ Nanoseconds int64 }
type Time struct{ Nanoseconds, OffsetSeconds int64 }
type LocalDateTime struct{ Seconds, Nanoseconds int64 }

// DateTime is an offset datetime wrapper. Seconds are local epoch seconds
// (UTC is Seconds - OffsetSeconds). Returned offset datetimes are time.Time.
type DateTime struct{ Seconds, Nanoseconds, OffsetSeconds int64 }

// ZonedDateTime is a named-zone datetime. UTC records the basis of Seconds:
// false means local epoch seconds in ZoneID (Bolt tag f), true means UTC epoch
// seconds (Bolt tag i). Values decoded from f are local-based and from i are
// UTC-based; the encoder writes the value's own basis on every protocol.
// Two values are equal only when basis, seconds, nanoseconds and zone match.
type ZonedDateTime struct {
	Seconds, Nanoseconds int64
	ZoneID               string
	UTC                  bool
}

// NewLocalZonedDateTime builds a named-zone datetime from local epoch seconds.
func NewLocalZonedDateTime(seconds, nanoseconds int64, zoneID string) ZonedDateTime {
	return ZonedDateTime{seconds, nanoseconds, zoneID, false}
}

// NewUTCZonedDateTime builds a named-zone datetime from UTC epoch seconds.
func NewUTCZonedDateTime(seconds, nanoseconds int64, zoneID string) ZonedDateTime {
	return ZonedDateTime{seconds, nanoseconds, zoneID, true}
}

// ZonedDateTimeFromTime builds a UTC-based value from t in the named zone.
func ZonedDateTimeFromTime(t time.Time, zoneID string) ZonedDateTime {
	return NewUTCZonedDateTime(t.Unix(), int64(t.Nanosecond()), zoneID)
}

// Time converts the value using the platform time zone database. A
// local-based value inside a DST overlap or gap resolves as time.Date does.
func (z ZonedDateTime) Time() (time.Time, error) {
	loc, e := time.LoadLocation(z.ZoneID)
	if e != nil {
		return time.Time{}, e
	}
	if z.UTC {
		return time.Unix(z.Seconds, z.Nanoseconds).In(loc), nil
	}
	w := time.Unix(z.Seconds, z.Nanoseconds).UTC()
	return time.Date(w.Year(), w.Month(), w.Day(), w.Hour(), w.Minute(), w.Second(), w.Nanosecond(), loc), nil
}

type Duration struct{ Months, Days, Seconds, Nanoseconds int64 }
type Point2D struct {
	SRID int64
	X, Y float64
}
type Point3D struct {
	SRID    int64
	X, Y, Z float64
}

func idText(id int64, elementID string) string {
	if elementID != "" {
		return elementID
	}
	return strconv.FormatInt(id, 10)
}

func dehydrate(v any, bolt5 bool) any {
	switch x := v.(type) {
	case Spatial:
		return x.ToMap()
	case time.Time:
		_, offset := x.Zone()
		if bolt5 {
			return Structure{0x49, []any{x.Unix(), int64(x.Nanosecond()), int64(offset)}}
		}
		return Structure{0x46, []any{x.Unix() + int64(offset), int64(x.Nanosecond()), int64(offset)}}
	case time.Duration:
		return Structure{0x45, []any{int64(0), int64(0), int64(x / time.Second), int64(x % time.Second)}}
	case Node:
		if bolt5 {
			return Structure{0x4e, []any{x.ID, x.Labels, x.Properties, idText(x.ID, x.ElementID)}}
		}
		return Structure{0x4e, []any{x.ID, x.Labels, x.Properties}}
	case Relationship:
		if bolt5 {
			return Structure{0x52, []any{x.ID, x.StartID, x.EndID, x.Type, x.Properties,
				idText(x.ID, x.ElementID), idText(x.StartID, x.StartElementID), idText(x.EndID, x.EndElementID)}}
		}
		return Structure{0x52, []any{x.ID, x.StartID, x.EndID, x.Type, x.Properties}}
	case UnboundRelationship:
		if bolt5 {
			return Structure{0x72, []any{x.ID, x.Type, x.Properties, idText(x.ID, x.ElementID)}}
		}
		return Structure{0x72, []any{x.ID, x.Type, x.Properties}}
	case Path:
		return Structure{0x50, []any{x.Nodes, x.Relationships, x.Sequence}}
	case Date:
		return Structure{0x44, []any{x.Days}}
	case LocalTime:
		return Structure{0x74, []any{x.Nanoseconds}}
	case Time:
		return Structure{0x54, []any{x.Nanoseconds, x.OffsetSeconds}}
	case LocalDateTime:
		return Structure{0x64, []any{x.Seconds, x.Nanoseconds}}
	case DateTime:
		if bolt5 {
			return Structure{0x49, []any{x.Seconds - x.OffsetSeconds, x.Nanoseconds, x.OffsetSeconds}}
		}
		return Structure{0x46, []any{x.Seconds, x.Nanoseconds, x.OffsetSeconds}}
	case ZonedDateTime:
		if x.UTC {
			return Structure{0x69, []any{x.Seconds, x.Nanoseconds, x.ZoneID}}
		}
		return Structure{0x66, []any{x.Seconds, x.Nanoseconds, x.ZoneID}}
	case Duration:
		return Structure{0x45, []any{x.Months, x.Days, x.Seconds, x.Nanoseconds}}
	case Point2D:
		return Structure{0x58, []any{x.SRID, x.X, x.Y}}
	case Point3D:
		return Structure{0x59, []any{x.SRID, x.X, x.Y, x.Z}}
	}
	return v
}

func checkDateTime(nanos, offset int64) {
	if nanos < 0 || nanos >= 1e9 || offset <= -86400 || offset >= 86400 {
		panic("invalid datetime")
	}
}

// Hydration assertions are guarded by Decode's malformed-input recovery.
func hydrate(t byte, f []any) any {
	counts := map[byte][]int{0x4e: {3, 4}, 0x52: {5, 8}, 0x72: {3, 4}, 0x50: {3}, 0x44: {1}, 0x74: {1}, 0x54: {2}, 0x64: {2},
		0x46: {3}, 0x66: {3}, 0x49: {3}, 0x69: {3}, 0x45: {4}, 0x58: {3}, 0x59: {4}}
	if ns, ok := counts[t]; ok {
		valid := false
		for _, n := range ns {
			valid = valid || len(f) == n
		}
		if !valid {
			panic("invalid structure field count")
		}
	}
	str := func(i int, id int64) string {
		if i < len(f) {
			return f[i].(string)
		}
		return strconv.FormatInt(id, 10)
	}
	switch t {
	case 0x4e:
		id := f[0].(int64)
		return Node{id, f[1].([]any), f[2].(map[string]any), str(3, id)}
	case 0x52:
		id, start, end := f[0].(int64), f[1].(int64), f[2].(int64)
		return Relationship{id, start, end, f[3].(string), f[4].(map[string]any), str(5, id), str(6, start), str(7, end)}
	case 0x72:
		id := f[0].(int64)
		return UnboundRelationship{id, f[1].(string), f[2].(map[string]any), str(3, id)}
	case 0x50:
		return Path{f[0].([]any), f[1].([]any), f[2].([]any)}
	case 0x44:
		return Date{f[0].(int64)}
	case 0x74:
		return LocalTime{f[0].(int64)}
	case 0x54:
		return Time{f[0].(int64), f[1].(int64)}
	case 0x64:
		return LocalDateTime{f[0].(int64), f[1].(int64)}
	case 0x46, 0x49:
		seconds, nanos, offset := f[0].(int64), f[1].(int64), f[2].(int64)
		checkDateTime(nanos, offset)
		if t == 0x46 {
			seconds -= offset
		}
		return time.Unix(seconds, nanos).In(time.FixedZone("", int(offset)))
	case 0x66, 0x69:
		return ZonedDateTime{f[0].(int64), f[1].(int64), f[2].(string), t == 0x69}
	case 0x45:
		return Duration{f[0].(int64), f[1].(int64), f[2].(int64), f[3].(int64)}
	case 0x58:
		return Point2D{f[0].(int64), f[1].(float64), f[2].(float64)}
	case 0x59:
		return Point3D{f[0].(int64), f[1].(float64), f[2].(float64), f[3].(float64)}
	}
	return Structure{t, f}
}
