// Package galactus is a standard-library-only Bolt 4.4 driver.
package galactus

import "time"

type Structure struct {
	Tag    byte
	Fields []any
}
type Node struct {
	ID         int64
	Labels     []any
	Properties map[string]any
}
type Relationship struct {
	ID, StartID, EndID int64
	Type               string
	Properties         map[string]any
}
type UnboundRelationship struct {
	ID         int64
	Type       string
	Properties map[string]any
}
type Path struct{ Nodes, Relationships, Sequence []any }
type Date struct{ Days int64 }
type LocalTime struct{ Nanoseconds int64 }
type Time struct{ Nanoseconds, OffsetSeconds int64 }
type LocalDateTime struct{ Seconds, Nanoseconds int64 }
type DateTime struct{ Seconds, Nanoseconds, OffsetSeconds int64 }
type ZonedDateTime struct {
	Seconds, Nanoseconds int64
	ZoneID               string
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

func dehydrate(v any) any {
	switch x := v.(type) {
	case Spatial:
		return x.ToMap()
	case time.Time:
		_, offset := x.Zone()
		return Structure{0x46, []any{x.Unix() + int64(offset), int64(x.Nanosecond()), int64(offset)}}
	case time.Duration:
		return Structure{0x45, []any{int64(0), int64(0), int64(x / time.Second), int64(x % time.Second)}}
	case Node:
		return Structure{0x4e, []any{x.ID, x.Labels, x.Properties}}
	case Relationship:
		return Structure{0x52, []any{x.ID, x.StartID, x.EndID, x.Type, x.Properties}}
	case UnboundRelationship:
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
		return Structure{0x46, []any{x.Seconds, x.Nanoseconds, x.OffsetSeconds}}
	case ZonedDateTime:
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

// Hydration assertions are guarded by Decode's malformed-input recovery.
func hydrate(t byte, f []any) any {
	counts := map[byte]int{0x4e: 3, 0x52: 5, 0x72: 3, 0x50: 3, 0x44: 1, 0x74: 1, 0x54: 2, 0x64: 2, 0x46: 3, 0x66: 3, 0x45: 4, 0x58: 3, 0x59: 4}
	if n, ok := counts[t]; ok && len(f) != n {
		panic("invalid structure field count")
	}
	switch t {
	case 0x4e:
		return Node{f[0].(int64), f[1].([]any), f[2].(map[string]any)}
	case 0x52:
		return Relationship{f[0].(int64), f[1].(int64), f[2].(int64), f[3].(string), f[4].(map[string]any)}
	case 0x72:
		return UnboundRelationship{f[0].(int64), f[1].(string), f[2].(map[string]any)}
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
	case 0x46:
		seconds, nanos, offset := f[0].(int64), f[1].(int64), f[2].(int64)
		if nanos < 0 || nanos >= 1e9 || offset <= -86400 || offset >= 86400 {
			panic("invalid datetime")
		}
		return time.Unix(seconds-offset, nanos).In(time.FixedZone("", int(offset)))
	case 0x66:
		return ZonedDateTime{f[0].(int64), f[1].(int64), f[2].(string)}
	case 0x45:
		return Duration{f[0].(int64), f[1].(int64), f[2].(int64), f[3].(int64)}
	case 0x58:
		return Point2D{f[0].(int64), f[1].(float64), f[2].(float64)}
	case 0x59:
		return Point3D{f[0].(int64), f[1].(float64), f[2].(float64), f[3].(float64)}
	}
	return Structure{t, f}
}
