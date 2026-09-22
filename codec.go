package galactus

import (
	"encoding/binary"
	"fmt"
	"math"
	"reflect"
	"unicode/utf8"
)

const MaxMessage = 64 * 1024 * 1024

func validateParameters(v any, depth int) error {
	if depth >= 64 {
		return fmt.Errorf("nesting exceeds 64 levels")
	}
	v = dehydrate(v)
	if v == nil {
		return nil
	}
	if s, ok := v.(Structure); ok {
		switch s.Tag {
		case 0x44, 0x74, 0x54, 0x64, 0x46, 0x66, 0x45, 0x58, 0x59:
		default:
			return fmt.Errorf("graph entities and unknown structures are result-only; pass properties or an ID")
		}
		for _, f := range s.Fields {
			if e := validateParameters(f, depth+1); e != nil {
				return e
			}
		}
		return nil
	}
	r := reflect.ValueOf(v)
	switch r.Kind() {
	case reflect.Map:
		iter := r.MapRange()
		for iter.Next() {
			if e := validateParameters(iter.Value().Interface(), depth+1); e != nil {
				return e
			}
		}
	case reflect.Slice, reflect.Array:
		if _, ok := v.([]byte); !ok {
			for i := 0; i < r.Len(); i++ {
				if e := validateParameters(r.Index(i).Interface(), depth+1); e != nil {
					return e
				}
			}
		}
	}
	return nil
}

func Encode(value any) ([]byte, error) {
	out := []byte{}
	var put func(any, int) error
	header := func(n int, tiny, base byte) {
		if tiny != 0 && n < 16 {
			out = append(out, tiny|byte(n))
		} else if n < 256 {
			out = append(out, base, byte(n))
		} else if n < 65536 {
			out = append(out, base+1, byte(n>>8), byte(n))
		} else {
			out = append(out, base+2)
			out = binary.BigEndian.AppendUint32(out, uint32(n))
		}
	}
	put = func(v any, depth int) error {
		if depth >= 64 {
			return fmt.Errorf("nesting exceeds 64 levels")
		}
		v = dehydrate(v)
		if v == nil {
			out = append(out, 0xc0)
			return nil
		}
		switch x := v.(type) {
		case Structure:
			if len(x.Fields) > 15 {
				return fmt.Errorf("invalid structure")
			}
			out = append(out, 0xb0|byte(len(x.Fields)), x.Tag)
			for _, f := range x.Fields {
				if e := put(f, depth+1); e != nil {
					return e
				}
			}
			return nil
		case []byte:
			header(len(x), 0, 0xcc)
			out = append(out, x...)
			return nil
		}
		r := reflect.ValueOf(v)
		switch r.Kind() {
		case reflect.Bool:
			if r.Bool() {
				out = append(out, 0xc3)
			} else {
				out = append(out, 0xc2)
			}
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			i := r.Int()
			switch {
			case i >= -16 && i <= 127:
				out = append(out, byte(i))
			case i >= -128 && i <= 127:
				out = append(out, 0xc8, byte(i))
			case i >= -32768 && i <= 32767:
				out = append(out, 0xc9)
				out = binary.BigEndian.AppendUint16(out, uint16(i))
			case i >= math.MinInt32 && i <= math.MaxInt32:
				out = append(out, 0xca)
				out = binary.BigEndian.AppendUint32(out, uint32(i))
			default:
				out = append(out, 0xcb)
				out = binary.BigEndian.AppendUint64(out, uint64(i))
			}
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			if r.Uint() > math.MaxInt64 {
				return fmt.Errorf("integer outside signed 64-bit range")
			}
			return put(int64(r.Uint()), depth+1)
		case reflect.Float32, reflect.Float64:
			out = append(out, 0xc1)
			out = binary.BigEndian.AppendUint64(out, math.Float64bits(r.Float()))
		case reflect.String:
			s := r.String()
			if !utf8.ValidString(s) {
				return fmt.Errorf("invalid UTF-8")
			}
			header(len(s), 0x80, 0xd0)
			out = append(out, s...)
		case reflect.Slice, reflect.Array:
			header(r.Len(), 0x90, 0xd4)
			for i := 0; i < r.Len(); i++ {
				if e := put(r.Index(i).Interface(), depth+1); e != nil {
					return e
				}
			}
		case reflect.Map:
			if r.Type().Key().Kind() != reflect.String {
				return fmt.Errorf("map keys must be strings")
			}
			header(r.Len(), 0xa0, 0xd8)
			iter := r.MapRange()
			for iter.Next() {
				if e := put(iter.Key().String(), depth+1); e != nil {
					return e
				}
				if e := put(iter.Value().Interface(), depth+1); e != nil {
					return e
				}
			}
		default:
			return fmt.Errorf("unsupported parameter type %T", v)
		}
		if len(out) > MaxMessage {
			return fmt.Errorf("message exceeds 64 MiB")
		}
		return nil
	}
	err := put(value, 0)
	return out, err
}

func Decode(data []byte) (value any, err error) {
	defer func() {
		if recover() != nil {
			value = nil
			err = fmt.Errorf("malformed PackStream")
		}
	}()
	p := 0
	take := func(n int) []byte {
		if n < 0 || n > len(data)-p {
			panic("truncated")
		}
		b := data[p : p+n]
		p += n
		return b
	}
	var read func(int) any
	read = func(depth int) any {
		if depth >= 64 {
			panic("depth")
		}
		m := take(1)[0]
		if m <= 127 {
			return int64(m)
		}
		if m >= 240 {
			return int64(int8(m))
		}
		switch m {
		case 0xc0:
			return nil
		case 0xc2:
			return false
		case 0xc3:
			return true
		case 0xc1:
			return math.Float64frombits(binary.BigEndian.Uint64(take(8)))
		case 0xc8:
			return int64(int8(take(1)[0]))
		case 0xc9:
			return int64(int16(binary.BigEndian.Uint16(take(2))))
		case 0xca:
			return int64(int32(binary.BigEndian.Uint32(take(4))))
		case 0xcb:
			return int64(binary.BigEndian.Uint64(take(8)))
		}
		kind, n := m&0xf0, int(m&15)
		if m < 0x80 || m > 0xbf {
			base := byte(0)
			for _, b := range []byte{0xcc, 0xd0, 0xd4, 0xd8} {
				if m >= b && m <= b+2 {
					base = b
					break
				}
			}
			if base == 0 {
				panic("marker")
			}
			kind = map[byte]byte{0xcc: 0xcc, 0xd0: 0x80, 0xd4: 0x90, 0xd8: 0xa0}[base]
			switch m - base {
			case 0:
				n = int(take(1)[0])
			case 1:
				n = int(binary.BigEndian.Uint16(take(2)))
			case 2:
				n = int(binary.BigEndian.Uint32(take(4)))
			}
		}
		if kind == 0x80 {
			b := take(n)
			if !utf8.Valid(b) {
				panic("UTF-8")
			}
			return string(b)
		}
		if kind == 0xcc {
			return append([]byte{}, take(n)...)
		}
		if kind == 0xb0 {
			tag := take(1)[0]
			f := make([]any, n)
			for i := range f {
				f[i] = read(depth + 1)
			}
			return hydrate(tag, f)
		}
		if n > len(data)-p {
			panic("size")
		}
		if kind == 0x90 {
			f := make([]any, n)
			for i := range f {
				f[i] = read(depth + 1)
			}
			return f
		}
		result := map[string]any{}
		for i := 0; i < n; i++ {
			k := read(depth + 1).(string)
			if _, ok := result[k]; ok {
				panic("duplicate key")
			}
			result[k] = read(depth + 1)
		}
		return hydrateMap(result)
	}
	value = read(0)
	if p != len(data) {
		return nil, fmt.Errorf("trailing PackStream bytes")
	}
	return value, nil
}
