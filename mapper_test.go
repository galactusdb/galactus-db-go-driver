package galactus

// The mapper's shared test (docs/MAPPER.md), and its checks that need no database.

import (
	"errors"
	"os"
	"reflect"
	"testing"
	"time"
)

type Person struct {
	ID    int64   `galactus:"id,key"`
	Name  string  `galactus:"name"`
	Email *string `galactus:"email_address"`
	Age   *int64
}

type Company struct {
	Name string `galactus:",key"`
}

type WorksAt struct {
	Since int64
	Role  string
}

func (WorksAt) RelationshipType() string { return "WORKS_AT" }

type Note struct{ Text string }

type badLabel struct{ X int64 }

func (badLabel) NodeLabel() string { return "Bad`Label" }

type badField struct {
	X int64 "galactus:\"a`b\""
}

func pointer[T any](v T) *T { return &v }

func isMappingError(err error) bool {
	var mapping *MappingError
	return errors.As(err, &mapping)
}

func TestMapperConversion(t *testing.T) {
	ann := Person{ID: 1, Name: "Ann", Email: pointer("ann@example.com")}
	properties, err := PropertiesOf(ann)
	want := map[string]any{"id": int64(1), "name": "Ann", "email_address": "ann@example.com", "age": nil}
	if err != nil || !reflect.DeepEqual(properties, want) {
		t.Fatalf("properties %v %v", properties, err)
	}
	node := Node{ID: 7, Labels: []any{"Person"}, Properties: map[string]any{"id": int64(1), "name": "Ann", "email_address": "a@b", "extra": true}}
	got, err := FromNode[Person](node)
	if err != nil || !reflect.DeepEqual(got, Person{ID: 1, Name: "Ann", Email: pointer("a@b")}) {
		t.Fatalf("from node %+v %v", got, err)
	}
	if got, err := FromNode[Person](Node{Properties: map[string]any{"id": int64(2)}}); err != nil || !reflect.DeepEqual(got, Person{ID: 2}) {
		t.Fatalf("a node that lacks properties %+v %v", got, err)
	}
	if _, err := FromNode[Person](Node{Properties: map[string]any{"id": "one"}}); !isMappingError(err) {
		t.Fatalf("a value of the wrong kind %v", err)
	}
	for _, name := range [][2]string{{"Name", "name"}, {"ID", "id"}, {"URLPath", "urlPath"}, {"EmailAddress", "emailAddress"}} {
		if got := defaultProperty(name[0]); got != name[1] {
			t.Fatalf("%s is stored as %s, expected %s", name[0], got, name[1])
		}
	}
	if _, err := PropertiesOf(badLabel{}); !isMappingError(err) {
		t.Fatalf("a label with a backtick %v", err)
	}
	if _, err := PropertiesOf(badField{}); !isMappingError(err) {
		t.Fatalf("a property with a backtick %v", err)
	}
	if _, err := FromNode[WorksAt](node); !isMappingError(err) {
		t.Fatalf("a relationship type from a node %v", err)
	}
}

func TestMapperLive(t *testing.T) {
	uri := os.Getenv("GDB_TEST_URI")
	if uri == "" {
		t.Skip("set GDB_TEST_URI for disposable database")
	}
	d, err := ConnectWithOptions(uri, "gdb", os.Getenv("GDB_TEST_PASSWORD"), "", 30*time.Second,
		Options{TrustedCertificates: os.Getenv("GDB_TEST_CA_FILE")})
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	equal := func(got, want any) {
		t.Helper()
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("got %+v, want %+v", got, want)
		}
	}
	ids := func(people []Person, err error) []int64 {
		t.Helper()
		must(err)
		out := []int64{}
		for _, p := range people {
			out = append(out, p.ID)
		}
		return out
	}
	_, err = d.ExecuteQuery("MATCH (n) WHERE n:Person OR n:Company OR n:Note DETACH DELETE n", nil)
	must(err)
	m := NewMapper(d)

	// 2. create, find, and the stored property name
	ann := Person{ID: 1, Name: "Ann", Email: pointer("ann@example.com")}
	created, err := Create(m, ann)
	must(err)
	equal(created, ann)
	found, err := Find[Person](m, int64(1))
	must(err)
	equal(*found, ann)
	raw, err := d.ExecuteQuery("MATCH (p:Person {id: 1}) RETURN p.email_address AS e, p.email AS none", nil)
	must(err)
	equal(raw.Records[0], map[string]any{"e": "ann@example.com", "none": nil})
	// 3. save changes, and creates
	ann.Age = pointer(int64(41))
	saved, err := Save(m, ann)
	must(err)
	equal(saved, ann)
	bob, err := Save(m, Person{ID: 2, Name: "Bob"})
	must(err)
	equal(bob, Person{ID: 2, Name: "Bob"})
	// 4. a missing key
	missing, err := Find[Person](m, int64(99))
	must(err)
	if missing != nil {
		t.Fatal("found a person nobody saved")
	}
	// 5. find all
	_, err = Save(m, Person{ID: 3, Name: "Bob", Age: pointer(int64(41))})
	must(err)
	equal(ids(FindAll[Person](m, nil, -1)), []int64{1, 2, 3})
	equal(ids(FindAll[Person](m, map[string]any{"Name": "Bob"}, -1)), []int64{2, 3})
	equal(ids(FindAll[Person](m, map[string]any{"Name": "Bob", "Age": int64(41)}, -1)), []int64{3})
	equal(ids(FindAll[Person](m, nil, 2)), []int64{1, 2})
	if _, err := FindAll[Person](m, map[string]any{"Height": 1}, -1); !isMappingError(err) {
		t.Fatalf("a filter on a field the type lacks %v", err)
	}
	// 6. relate and related
	acme, err := Save(m, Company{Name: "Acme"})
	must(err)
	job, err := Relate(m, ann, WorksAt{Since: 2020, Role: "engineer"}, acme)
	must(err)
	equal(job, WorksAt{Since: 2020, Role: "engineer"})
	_, err = Relate(m, ann, WorksAt{Since: 2021, Role: "lead"}, acme)
	must(err)
	jobs, err := Related[WorksAt, Company](m, ann)
	must(err)
	equal(jobs, []Pair[WorksAt, Company]{{Relationship: WorksAt{Since: 2021, Role: "lead"}, Node: Company{Name: "Acme"}}})
	if _, err := Relate(m, Person{ID: 77, Name: "Nobody"}, WorksAt{Since: 1}, acme); !isMappingError(err) {
		t.Fatalf("relating a person who is not stored %v", err)
	}
	// 7. a hand-written statement
	equal(ids(Query[Person](m, "MATCH (p:Person) WHERE p.age >= $age RETURN p ORDER BY p.id", map[string]any{"age": int64(41)})), []int64{1, 3})
	made, err := Query[Company](m, "RETURN {name: $n} AS c", map[string]any{"n": "Made up"})
	must(err)
	equal(made, []Company{{Name: "Made up"}})
	// 8. delete
	deleted, err := Delete[Person](m, int64(3))
	must(err)
	again, err := Delete[Person](m, int64(3))
	must(err)
	equal([]bool{deleted, again}, []bool{true, false})
	// 9. no key
	note, err := Create(m, Note{Text: "hello"})
	must(err)
	equal(note, Note{Text: "hello"})
	if _, err := Save(m, Note{Text: "x"}); !isMappingError(err) {
		t.Fatalf("saving a type with no key %v", err)
	}
	if _, err := Find[Note](m, "x"); !isMappingError(err) {
		t.Fatalf("finding a type with no key %v", err)
	}
	if _, err := Find[Person](m, nil); !isMappingError(err) {
		t.Fatalf("finding by no key %v", err)
	}
	// the operations join an open transaction
	must(d.Begin(false))
	_, err = Save(m, Person{ID: 50, Name: "Temporary"})
	must(err)
	must(d.Rollback())
	gone, err := Find[Person](m, int64(50))
	must(err)
	if gone != nil {
		t.Fatal("a rolled-back save was kept")
	}
}
