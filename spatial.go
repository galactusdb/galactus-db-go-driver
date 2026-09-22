package galactus

import (
	"encoding/hex"
	"fmt"
)

// Spatial preserves all seven WKB shape families, XY/XYZ and typed empties.
// Use spatial.fromMap($shape) to promote a parameter into a database spatial value.
type Spatial struct {
	Domain        string
	SRID          int64
	Layout, Model string
	WKB           []byte
}

func (s Spatial) ToMap() map[string]any {
	return map[string]any{"$gdbType": "spatial", "version": int64(1), "domain": s.Domain, "srid": s.SRID, "layout": s.Layout, "model": s.Model, "wkb": s.WKB}
}
func SpatialFromMap(m map[string]any) (Spatial, error) {
	var s Spatial
	if m["$gdbType"] != "spatial" || m["version"] != int64(1) || len(m) != 7 {
		return s, fmt.Errorf("invalid spatial envelope")
	}
	var ok bool
	if s.Domain, ok = m["domain"].(string); !ok {
		return s, fmt.Errorf("invalid domain")
	}
	if s.SRID, ok = m["srid"].(int64); !ok {
		return s, fmt.Errorf("invalid SRID")
	}
	if s.Layout, ok = m["layout"].(string); !ok {
		return s, fmt.Errorf("invalid layout")
	}
	if s.Model, ok = m["model"].(string); !ok {
		return s, fmt.Errorf("invalid model")
	}
	if (s.Domain != "geometry" && s.Domain != "geography") || (s.Layout != "XY" && s.Layout != "XYZ") || (s.Domain == "geometry" && s.Model != "planar") || (s.Domain == "geography" && s.Model != "greatCircle") {
		return s, fmt.Errorf("unsupported spatial metadata")
	}
	if h, ok := m["wkbHex"].(string); ok {
		b, e := hex.DecodeString(h)
		if e != nil {
			return s, e
		}
		s.WKB = b
	} else if b, ok := m["wkb"].([]byte); ok {
		s.WKB = append([]byte{}, b...)
	} else {
		return s, fmt.Errorf("invalid WKB")
	}
	return s, nil
}
func hydrateMap(m map[string]any) any {
	if m["$gdbType"] == "spatial" && m["version"] == int64(1) {
		s, e := SpatialFromMap(m)
		if e != nil {
			panic(e)
		}
		return s
	}
	return m
}
