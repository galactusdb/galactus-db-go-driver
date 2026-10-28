package galactus

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"math"
	"os"
	"reflect"
	"strconv"
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
	text := strings.ReplaceAll(string(b), "\r\n", "\n")
	return strings.Split(strings.TrimSuffix(text, "\n"), "\n")
}

// roundTrip decodes each fixture and checks it re-encodes to the same bytes
// in the given protocol mode.
func roundTrip(t *testing.T, name string, bolt5 bool) {
	for _, line := range lines(t, name) {
		parts := strings.Split(line, "\t")
		b, _ := hex.DecodeString(parts[1])
		v, e := Decode(b)
		if e != nil {
			t.Fatal(parts[0], e)
		}
		encoded, e := EncodeProtocol(v, bolt5)
		if e != nil {
			t.Fatal(parts[0], e)
		}
		if !bytes.Equal(encoded, b) {
			t.Fatalf("%s: encoded %x, want %x", parts[0], encoded, b)
		}
		again, e := Decode(encoded)
		if e != nil || !reflect.DeepEqual(v, again) {
			t.Fatal(parts[0], e, v, again)
		}
	}
}

func TestGolden(t *testing.T) {
	roundTrip(t, "values.tsv", false)
	for _, h := range lines(t, "malformed.tsv") {
		b, _ := hex.DecodeString(h)
		if _, e := Decode(b); e == nil {
			t.Fatal("accepted malformed", h)
		}
	}
}

func TestBolt5(t *testing.T) {
	roundTrip(t, "bolt5.tsv", true)
	v, _ := Decode(mustHex(t, "b44e019186506572736f6ea1846e616d65834164618131"))
	if v.(Node).ElementID != "1" {
		t.Fatal("node element id")
	}
	r, _ := Decode(mustHex(t, "b5520201038552454c4154a0"))
	if rel := r.(Relationship); rel.ElementID != "2" || rel.StartElementID != "1" || rel.EndElementID != "3" {
		t.Fatal("4.4 relationship element ids", rel)
	}
	z, _ := Decode(mustHex(t, "b36900008d4575726f70652f4c6f6e646f6e"))
	if z != NewUTCZonedDateTime(0, 0, "Europe/London") {
		t.Fatal("i basis", z)
	}
}

func mustHex(t *testing.T, h string) []byte {
	b, e := hex.DecodeString(h)
	if e != nil {
		t.Fatal(e)
	}
	return b
}

func TestDatetimePairs(t *testing.T) {
	for _, line := range lines(t, "datetime_pairs.tsv") {
		parts := strings.Split(line, "\t")
		f, i := mustHex(t, parts[0]), mustHex(t, parts[1])
		a, e1 := Decode(f)
		b, e2 := Decode(i)
		if e1 != nil || e2 != nil {
			t.Fatal(line, e1, e2)
		}
		ta, tb := a.(time.Time), b.(time.Time)
		_, oa := ta.Zone()
		_, ob := tb.Zone()
		if !ta.Equal(tb) || oa != ob || !reflect.DeepEqual(a, b) {
			t.Fatal("pair differs", line, ta, tb)
		}
		for _, mode := range []struct {
			bolt5 bool
			want  []byte
		}{{false, f}, {true, i}} {
			got, e := EncodeProtocol(a, mode.bolt5)
			if e != nil || !bytes.Equal(got, mode.want) {
				t.Fatalf("%s bolt5=%v: %x", line, mode.bolt5, got)
			}
			wrapper := DateTime{ta.Unix() + int64(oa), int64(ta.Nanosecond()), int64(oa)}
			if got, _ = EncodeProtocol(wrapper, mode.bolt5); !bytes.Equal(got, mode.want) {
				t.Fatalf("DateTime wrapper bolt5=%v: %x", mode.bolt5, got)
			}
		}
	}
	// A named-zone value keeps its own basis on both protocols.
	for _, z := range []ZonedDateTime{NewLocalZonedDateTime(3600, 0, "Europe/London"), NewUTCZonedDateTime(0, 0, "Europe/London")} {
		a, _ := Encode(z)
		b, _ := EncodeProtocol(z, true)
		if !bytes.Equal(a, b) {
			t.Fatal("zoned basis changed with protocol")
		}
		if back, _ := Decode(a); back != z {
			t.Fatal("zoned round trip", back, z)
		}
	}
}

func TestErrorClassification(t *testing.T) {
	e := &DatabaseError{Code: SnapshotTooOld}
	if e.Classification() != "TransientError" || e.Category() != "Transaction" || e.Title() != "SnapshotTooOld" || !e.IsTransient() || !e.IsRetryable() || e.IsClientError() || e.IsDatabaseError() {
		t.Fatal("transient classification")
	}
	e = &DatabaseError{Code: ConstraintValidationFailed}
	if !e.IsClientError() || e.IsRetryable() || e.Title() != "ConstraintValidationFailed" {
		t.Fatal("client classification")
	}
	if !(&DatabaseError{Code: UnsupportedFeature}).IsDatabaseError() {
		t.Fatal("database classification")
	}
	e = &DatabaseError{Code: "Odd.Code"}
	if e.Classification() != "" || e.Category() != "" || e.Title() != "" || e.IsRetryable() {
		t.Fatal("short code")
	}
}

func TestConfiguration(t *testing.T) {
	var ce *ConfigError
	for _, uri := range []string{"bolt+ssc://localhost:7687", "neo4j://localhost:7687", "neo4j+s://localhost:7687", "neo4j+ssc://localhost:7687", "bolt://localhost:7687?x=1", "bolt://u:p@localhost:7687"} {
		if _, e := Connect(uri, "gdb", "x", "", time.Second); !errors.As(e, &ce) {
			t.Fatal("expected configuration error for", uri, e)
		}
	}
	if _, e := ConnectWithOptions("bolt://localhost:1", "gdb", "x", "", time.Second, Options{TrustedCertificates: "ca.pem"}); !errors.As(e, &ce) {
		t.Fatal("trusted certificates on bolt:// must be a configuration error", e)
	}
	if _, e := ConnectWithOptions("bolt+s://localhost:1", "gdb", "x", "", time.Second, Options{TrustedCertificates: "go.mod"}); !errors.As(e, &ce) {
		t.Fatal("a file without PEM certificates must be a configuration error", e)
	}
}

func isTLSError(e error) bool {
	var verify *tls.CertificateVerificationError
	var authority x509.UnknownAuthorityError
	var invalid x509.CertificateInvalidError
	var hostname x509.HostnameError
	return errors.As(e, &verify) || errors.As(e, &authority) || errors.As(e, &invalid) || errors.As(e, &hostname)
}

func TestLive(t *testing.T) {
	uri := os.Getenv("GDB_TEST_URI")
	if uri == "" {
		t.Skip("set GDB_TEST_URI for disposable database")
	}
	password := os.Getenv("GDB_TEST_PASSWORD")
	ca := os.Getenv("GDB_TEST_CA_FILE")
	d, e := ConnectWithOptions(uri, "gdb", password, "", 30*time.Second, Options{TrustedCertificates: ca})
	if e != nil {
		t.Fatal(e)
	}
	defer d.Close()
	if want := os.Getenv("GDB_TEST_EXPECT_BOLT"); want != "" && d.ProtocolVersion() != want {
		t.Fatalf("agreed Bolt %s, expected %s", d.ProtocolVersion(), want)
	}
	if d.ServerAgent() == "" {
		t.Fatal("missing server agent")
	}
	t.Logf("Bolt %s, server %s, connection %s", d.ProtocolVersion(), d.ServerAgent(), d.ConnectionID())
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

	// Datetimes: an offset datetime and a named-zone datetime echo unchanged.
	offset := time.Date(2026, 10, 28, 9, 30, 15, 123456789, time.FixedZone("", -18000))
	if back := query("RETURN $v AS v", map[string]any{"v": offset}).Records[0]["v"].(time.Time); !back.Equal(offset) || back.Format(time.RFC3339Nano) != offset.Format(time.RFC3339Nano) {
		t.Fatal("offset datetime", back)
	}
	utc := time.Date(2026, 6, 1, 11, 0, 0, 5, time.UTC).Unix() // 12:00 BST
	for _, z := range []ZonedDateTime{NewUTCZonedDateTime(utc, 5, "Europe/London"), NewLocalZonedDateTime(utc+3600, 5, "Europe/London")} {
		back := query("RETURN $v AS v", map[string]any{"v": z}).Records[0]["v"].(ZonedDateTime)
		want := NewLocalZonedDateTime(utc+3600, 5, "Europe/London") // 4.4 answers with f
		if d.ProtocolVersion() != "4.4" {
			want = NewUTCZonedDateTime(utc, 5, "Europe/London") // 5.x answers with i
		}
		if back != want {
			t.Fatal("named-zone datetime", z, back, want)
		}
	}

	// Element ids are the decimal text of the integer ids.
	row := query("CREATE (a:DriverGoElement)-[r:DRIVER_GO_LINK]->(b:DriverGoElement) RETURN a, r, b", nil).Records[0]
	a, r, b := row["a"].(Node), row["r"].(Relationship), row["b"].(Node)
	id := func(n int64) string { return strconv.FormatInt(n, 10) }
	if a.ElementID != id(a.ID) || b.ElementID != id(b.ID) || r.ElementID != id(r.ID) || r.StartElementID != id(a.ID) || r.EndElementID != id(b.ID) {
		t.Fatal("element ids", a, r, b)
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

	// A database error is classified and the same driver keeps working.
	var dbe *DatabaseError
	if _, e = d.ExecuteQuery("INVALID QUERY", nil); !errors.As(e, &dbe) {
		t.Fatal("expected database error", e)
	}
	if dbe.Code == "" || dbe.Classification() != "ClientError" || dbe.Category() == "" || dbe.Title() == "" || !dbe.IsClientError() || dbe.IsRetryable() {
		t.Fatal("classification", dbe.Code)
	}
	if query("RETURN 1 AS one", nil).Records[0]["one"] != int64(1) {
		t.Fatal("query after failure")
	}

	// A failure inside a transaction closes it; autocommit works afterwards.
	if e = d.Begin(false); e != nil {
		t.Fatal(e)
	}
	query("CREATE (:DriverGoTx {n:1})", nil)
	if _, e = d.ExecuteQuery("INVALID QUERY", nil); !errors.As(e, &dbe) {
		t.Fatal("expected database error in transaction", e)
	}
	if _, e = d.Commit(); e == nil {
		t.Fatal("transaction should be closed after a failure")
	}
	if query("MATCH (n:DriverGoTx) RETURN count(n) AS n", nil).Records[0]["n"] != int64(0) {
		t.Fatal("failed transaction was not rolled back")
	}

	// Unique constraint violation.
	query("CREATE CONSTRAINT FOR (n:DriverGoUnique) REQUIRE n.key IS UNIQUE", nil)
	query("CREATE (:DriverGoUnique {key:'k'})", nil)
	if _, e = d.ExecuteQuery("CREATE (:DriverGoUnique {key:'k'})", nil); !errors.As(e, &dbe) || dbe.Code != ConstraintValidationFailed || dbe.Title() != "ConstraintValidationFailed" {
		t.Fatal("expected constraint violation", e)
	}

	// Decimals arrive as strings holding their exact digits.
	if got := query("RETURN decimal($d) AS d", map[string]any{"d": "12.30"}).Records[0]["d"]; got != "12.30" {
		t.Fatalf("decimal %#v", got)
	}
	if d.LastBookmark() == "" {
		t.Log("server sent no bookmark")
	}

	if bad, e := ConnectWithOptions(uri, "gdb", "wrong-password", "", 30*time.Second, Options{TrustedCertificates: ca}); e == nil {
		bad.Close()
		t.Fatal("expected auth failure")
	} else if !errors.As(e, &dbe) {
		t.Fatal("auth failure should be a database error", e)
	}

	if ca != "" {
		if bad, e := Connect(uri, "gdb", password, "", 30*time.Second); e == nil {
			bad.Close()
			t.Fatal("expected certificate verification failure without the trusted certificate")
		} else if !isTLSError(e) {
			t.Fatal("expected a TLS certificate error", e)
		}
		plain := strings.Replace(uri, "bolt+s://", "bolt://", 1)
		var ce *ConfigError
		if _, e := ConnectWithOptions(plain, "gdb", password, "", 30*time.Second, Options{TrustedCertificates: ca}); !errors.As(e, &ce) {
			t.Fatal("trusted certificates on bolt:// must be a configuration error", e)
		}
	}
}
