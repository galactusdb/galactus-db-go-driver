package galactus

import (
	"encoding/hex"
	"math"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

func lines(t *testing.T, name string) []string {
	t.Helper()
	b, e := os.ReadFile("tests/fixtures/" + name)
	if e != nil {
		t.Fatal(e)
	}
	return strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
}
func TestGolden(t *testing.T) {
	for _, line := range lines(t, "values.tsv") {
		parts := strings.Split(line, "\t")
		b, _ := hex.DecodeString(parts[1])
		v, e := Decode(b)
		if e != nil {
			t.Fatal(parts[0], e)
		}
		encoded, e := Encode(v)
		if e != nil {
			t.Fatal(e)
		}
		again, e := Decode(encoded)
		if e != nil || !reflect.DeepEqual(v, again) {
			t.Fatal(parts[0], e, v, again)
		}
	}
	for _, h := range lines(t, "malformed.tsv") {
		b, _ := hex.DecodeString(h)
		if _, e := Decode(b); e == nil {
			t.Fatal("accepted malformed", h)
		}
	}
}
func TestLive(t *testing.T) {
	uri := os.Getenv("GDB_TEST_URI")
	if uri == "" {
		t.Skip("set GDB_TEST_URI for disposable database")
	}
	password := os.Getenv("GDB_TEST_PASSWORD")
	d, e := Connect(uri, "gdb", password, "neo4j", 30*time.Second)
	if e != nil {
		t.Fatal(e)
	}
	defer d.Close()
	query := func(q string, p map[string]any) Result {
		t.Helper()
		r, e := d.ExecuteQuery(q, p)
		if e != nil {
			t.Fatal(e)
		}
		return r
	}
	v := map[string]any{"n": int64(math.MaxInt64), "b": []byte{0, 255}, "s": strings.Repeat("x", 70000), "t": time.Date(1960, 1, 1, 1, 2, 3, 456789123, time.FixedZone("", 19800))}
	got := query("RETURN $v AS v", map[string]any{"v": v}).Records[0]["v"].(map[string]any)
	if !reflect.DeepEqual(v, got) {
		t.Fatal("native mapping mismatch")
	}
	for _, line := range lines(t, "spatial.tsv") {
		parts := strings.Split(line, "\t")
		shape := query("RETURN spatial.fromWKT($wkt,{domain:$domain}) AS shape", map[string]any{"domain": parts[0], "wkt": parts[1]}).Records[0]["shape"].(Spatial)
		row := query("RETURN spatial.fromMap($s) AS shape, $nested AS nested", map[string]any{"s": shape, "nested": []any{map[string]any{"shape": shape}}}).Records[0]
		if !reflect.DeepEqual(row["shape"], shape) || !reflect.DeepEqual(row["nested"].([]any)[0].(map[string]any)["shape"], shape) {
			t.Fatal("spatial mismatch", line)
		}
	}
	if e = d.Begin(false); e != nil {
		t.Fatal(e)
	}
	query("CREATE (:DriverGo {n:1})", nil)
	if e = d.Rollback(); e != nil {
		t.Fatal(e)
	}
	if query("MATCH (n:DriverGo) RETURN count(n) AS n", nil).Records[0]["n"] != int64(0) {
		t.Fatal("rollback")
	}
	if e = d.Begin(false); e != nil {
		t.Fatal(e)
	}
	query("CREATE (:DriverGo {n:2})", nil)
	if _, e = d.Commit(); e != nil {
		t.Fatal(e)
	}
	if query("MATCH (n:DriverGo) RETURN n", nil).Records[0]["n"].(Node).Properties["n"] != int64(2) {
		t.Fatal("commit")
	}
	if _, e = d.ExecuteQuery("INVALID QUERY", nil); e == nil {
		t.Fatal("expected database error")
	}
	if _, e = d.ExecuteQuery("RETURN 1", nil); e == nil {
		t.Fatal("expected closed connection")
	}
	if bad, e := Connect(uri, "gdb", "wrong-password", "neo4j", 30*time.Second); e == nil {
		bad.Close()
		t.Fatal("expected auth failure")
	}
}
