package galactus

// Object mapping: nodes and relationships as the application's own structs,
// and the Cypher for everyday operations. Standard library only;
// docs/MAPPER.md is the contract every Galactus mapper follows.
//
//	type Person struct {
//		ID    int64   `galactus:"id,key"`
//		Name  string  `galactus:"name"`
//		Email *string `galactus:"email_address"`
//	}
//
//	people := galactus.NewMapper(driver)
//	ann, err := galactus.Save(people, Person{ID: 1, Name: "Ann"})
//	found, err := galactus.Find[Person](people, int64(1))
//
// A struct is a node type as it stands: its label is its name, or what a
// NodeLabel method returns. A struct with a RelationshipType method is a
// relationship type. The tag gives a field's property name, with ",key" on
// the field that identifies a node; a field with no tag is stored under its
// name with a lower-case start, and one tagged "-" is not stored.

import (
	"fmt"
	"math"
	"reflect"
	"strings"
	"sync"
	"unicode"
)

// MappingError is a type that cannot be mapped, or a value that does not fit it.
type MappingError struct{ Message string }

func (e *MappingError) Error() string { return e.Message }

func mappingError(format string, args ...any) error {
	return &MappingError{Message: fmt.Sprintf(format, args...)}
}

// Labelled names the label of a node type that is not its struct name.
type Labelled interface{ NodeLabel() string }

// Typed marks a struct as a relationship type and names its type.
type Typed interface{ RelationshipType() string }

type mappedField struct {
	index  int
	name   string // the Go field
	stored string // the property
}

type shape struct {
	goType       reflect.Type
	name         string // label or relationship type
	relationship bool
	fields       []mappedField
	key          *mappedField
}

var shapes sync.Map // reflect.Type -> *shape or error

func checkName(value, what string) error {
	if value == "" || strings.Contains(value, "`") {
		return mappingError("%s must be a name without backticks, got %q", what, value)
	}
	return nil
}

func quoteName(name string) string { return "`" + name + "`" }

// defaultProperty lowers the leading capitals of a field name: Name is name,
// ID is id and URLPath is urlPath.
func defaultProperty(field string) string {
	runes := []rune(field)
	upper := 0
	for upper < len(runes) && unicode.IsUpper(runes[upper]) {
		upper++
	}
	if upper > 1 && upper < len(runes) {
		upper--
	}
	for i := 0; i < upper; i++ {
		runes[i] = unicode.ToLower(runes[i])
	}
	return string(runes)
}

func shapeOf(t reflect.Type) (*shape, error) {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if found, ok := shapes.Load(t); ok {
		if err, failed := found.(error); failed {
			return nil, err
		}
		return found.(*shape), nil
	}
	s, err := describe(t)
	if err != nil {
		shapes.Store(t, err)
		return nil, err
	}
	shapes.Store(t, s)
	return s, nil
}

func describe(t reflect.Type) (*shape, error) {
	if t.Kind() != reflect.Struct {
		return nil, mappingError("%s is not a struct and cannot be mapped", t)
	}
	s := &shape{goType: t, name: t.Name()}
	zero := reflect.New(t).Interface()
	if typed, ok := zero.(Typed); ok {
		s.relationship, s.name = true, typed.RelationshipType()
	} else if labelled, ok := zero.(Labelled); ok {
		s.name = labelled.NodeLabel()
	}
	if err := checkName(s.name, t.Name()+"'s label or relationship type"); err != nil {
		return nil, err
	}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		tag := f.Tag.Get("galactus")
		if tag == "-" {
			continue
		}
		stored, options, _ := strings.Cut(tag, ",")
		if stored == "" {
			stored = defaultProperty(f.Name)
		}
		if err := checkName(stored, t.Name()+"."+f.Name); err != nil {
			return nil, err
		}
		s.fields = append(s.fields, mappedField{index: i, name: f.Name, stored: stored})
		if options == "key" {
			if s.key != nil {
				return nil, mappingError("%s has more than one key", t.Name())
			}
			s.key = &s.fields[len(s.fields)-1]
		} else if options != "" {
			return nil, mappingError("%s.%s: unknown tag option %q", t.Name(), f.Name, options)
		}
	}
	// A slice that grows moves its elements: take the key's address again.
	if s.key != nil {
		for i := range s.fields {
			if s.fields[i].index == s.key.index {
				s.key = &s.fields[i]
			}
		}
	}
	return s, nil
}

func nodeShape(t reflect.Type) (*shape, error) {
	s, err := shapeOf(t)
	if err == nil && s.relationship {
		return nil, mappingError("%s is a relationship type, not a node type", s.goType.Name())
	}
	return s, err
}

func relationshipShape(t reflect.Type) (*shape, error) {
	s, err := shapeOf(t)
	if err == nil && !s.relationship {
		return nil, mappingError("%s is not a relationship type: it has no RelationshipType method", s.goType.Name())
	}
	return s, err
}

func keyedShape(t reflect.Type) (*shape, error) {
	s, err := nodeShape(t)
	if err == nil && s.key == nil {
		return nil, mappingError("%s has no key", s.goType.Name())
	}
	return s, err
}

func structValue(v any) reflect.Value {
	value := reflect.ValueOf(v)
	for value.Kind() == reflect.Pointer && !value.IsNil() {
		value = value.Elem()
	}
	return value
}

func plain(field reflect.Value) any {
	for field.Kind() == reflect.Pointer {
		if field.IsNil() {
			return nil
		}
		field = field.Elem()
	}
	return field.Interface()
}

// PropertiesOf gives every mapped property of a struct, by its stored name.
func PropertiesOf(v any) (map[string]any, error) {
	value := structValue(v)
	if !value.IsValid() {
		return nil, mappingError("nothing to map")
	}
	s, err := shapeOf(value.Type())
	if err != nil {
		return nil, err
	}
	out := make(map[string]any, len(s.fields))
	for _, f := range s.fields {
		out[f.stored] = plain(value.Field(f.index))
	}
	return out, nil
}

func keyOf(s *shape, v any) (any, error) {
	return checkKey(s, plain(structValue(v).Field(s.key.index)))
}

func checkKey(s *shape, key any) (any, error) {
	if key == nil {
		return nil, mappingError("%s: the key %s has no value", s.goType.Name(), s.key.name)
	}
	return key, nil
}

// assign puts a database value into a field, converting between the number
// widths Go has and the two the database has.
func assign(field reflect.Value, value any) error {
	if value == nil {
		field.SetZero()
		return nil
	}
	if field.Kind() == reflect.Pointer {
		target := reflect.New(field.Type().Elem())
		if err := assign(target.Elem(), value); err != nil {
			return err
		}
		field.Set(target)
		return nil
	}
	given := reflect.ValueOf(value)
	if given.Type().AssignableTo(field.Type()) {
		field.Set(given)
		return nil
	}
	switch field.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if n, ok := value.(int64); ok && !field.OverflowInt(n) {
			field.SetInt(n)
			return nil
		}
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		if n, ok := value.(int64); ok && n >= 0 && !field.OverflowUint(uint64(n)) {
			field.SetUint(uint64(n))
			return nil
		}
	case reflect.Float32, reflect.Float64:
		switch n := value.(type) {
		case float64:
			field.SetFloat(n)
			return nil
		case int64:
			if float64(n) <= math.MaxFloat64 {
				field.SetFloat(float64(n))
				return nil
			}
		}
	case reflect.Slice:
		if items, ok := value.([]any); ok {
			out := reflect.MakeSlice(field.Type(), len(items), len(items))
			for i, item := range items {
				if err := assign(out.Index(i), item); err != nil {
					return err
				}
			}
			field.Set(out)
			return nil
		}
	default:
		if given.Type().ConvertibleTo(field.Type()) && given.Kind() == field.Kind() {
			field.Set(given.Convert(field.Type()))
			return nil
		}
	}
	return fmt.Errorf("a %T does not fit a %s", value, field.Type())
}

func build[T any](s *shape, properties map[string]any) (T, error) {
	var out T
	value := reflect.ValueOf(&out).Elem()
	for _, f := range s.fields {
		if err := assign(value.Field(f.index), properties[f.stored]); err != nil {
			return out, mappingError("%s.%s: %v", s.goType.Name(), f.name, err)
		}
	}
	return out, nil
}

func typeOf[T any]() reflect.Type { return reflect.TypeOf((*T)(nil)).Elem() }

// FromNode makes a T of a node's properties.
func FromNode[T any](node Node) (T, error) {
	s, err := nodeShape(typeOf[T]())
	if err != nil {
		var zero T
		return zero, err
	}
	return build[T](s, node.Properties)
}

// FromRelationship makes a T of a relationship's properties.
func FromRelationship[T any](relationship Relationship) (T, error) {
	s, err := relationshipShape(typeOf[T]())
	if err != nil {
		var zero T
		return zero, err
	}
	return build[T](s, relationship.Properties)
}

func fromValue[T any](value any) (T, error) {
	var zero T
	switch v := value.(type) {
	case Node:
		return FromNode[T](v)
	case Relationship:
		return FromRelationship[T](v)
	case UnboundRelationship:
		return FromRelationship[T](Relationship{Properties: v.Properties})
	case map[string]any:
		s, err := shapeOf(typeOf[T]())
		if err != nil {
			return zero, err
		}
		return build[T](s, v)
	}
	return zero, mappingError("%s: expected a node, a relationship or a map, got %T", typeOf[T]().Name(), value)
}

// Mapper runs the everyday operations on a driver, each as one statement.
type Mapper struct{ Driver *Driver }

// NewMapper makes a mapper that runs on the driver.
func NewMapper(driver *Driver) *Mapper { return &Mapper{Driver: driver} }

func matchKey(variable string, s *shape, parameter string) string {
	return fmt.Sprintf("(%s:%s {%s: $%s})", variable, quoteName(s.name), quoteName(s.key.stored), parameter)
}

// Create makes a new node of the object and returns it as stored.
func Create[T any](m *Mapper, object T) (T, error) {
	var zero T
	s, err := nodeShape(typeOf[T]())
	if err != nil {
		return zero, err
	}
	properties, err := PropertiesOf(object)
	if err != nil {
		return zero, err
	}
	result, err := m.Driver.ExecuteQuery("CREATE (n:"+quoteName(s.name)+") SET n = $properties RETURN n",
		map[string]any{"properties": properties})
	if err != nil {
		return zero, err
	}
	return fromValue[T](result.Records[0]["n"])
}

// Save creates the node with the object's key, or changes it, and returns it as stored.
func Save[T any](m *Mapper, object T) (T, error) {
	var zero T
	s, err := keyedShape(typeOf[T]())
	if err != nil {
		return zero, err
	}
	key, err := keyOf(s, object)
	if err != nil {
		return zero, err
	}
	properties, err := PropertiesOf(object)
	if err != nil {
		return zero, err
	}
	result, err := m.Driver.ExecuteQuery("MERGE "+matchKey("n", s, "key")+" SET n += $properties RETURN n",
		map[string]any{"key": key, "properties": properties})
	if err != nil {
		return zero, err
	}
	return fromValue[T](result.Records[0]["n"])
}

// Find returns the node with the key, or nil when there is none.
func Find[T any](m *Mapper, key any) (*T, error) {
	s, err := keyedShape(typeOf[T]())
	if err != nil {
		return nil, err
	}
	if key, err = checkKey(s, key); err != nil {
		return nil, err
	}
	result, err := m.Driver.ExecuteQuery("MATCH "+matchKey("n", s, "key")+" RETURN n LIMIT 1", map[string]any{"key": key})
	if err != nil || len(result.Records) == 0 {
		return nil, err
	}
	found, err := fromValue[T](result.Records[0]["n"])
	if err != nil {
		return nil, err
	}
	return &found, nil
}

// FindAll returns every node of the type whose properties equal those in
// filter, which is keyed by Go field name. A negative limit is no limit.
func FindAll[T any](m *Mapper, filter map[string]any, limit int) ([]T, error) {
	s, err := nodeShape(typeOf[T]())
	if err != nil {
		return nil, err
	}
	parameters := map[string]any{}
	var conditions []string
	for _, f := range s.fields {
		value, wanted := filter[f.name]
		if !wanted {
			continue
		}
		parameter := fmt.Sprintf("p%d", len(conditions))
		parameters[parameter] = value
		conditions = append(conditions, "n."+quoteName(f.stored)+" = $"+parameter)
	}
	if len(conditions) != len(filter) {
		return nil, mappingError("%s: the filter names a field the type does not have", s.goType.Name())
	}
	query := "MATCH (n:" + quoteName(s.name) + ")"
	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}
	query += " RETURN n"
	if s.key != nil {
		query += " ORDER BY n." + quoteName(s.key.stored)
	}
	if limit >= 0 {
		parameters["limit"] = int64(limit)
		query += " LIMIT $limit"
	}
	return queryAs[T](m, query, parameters, "n")
}

// Delete removes the node with the key and its relationships. It reports
// whether there was one.
func Delete[T any](m *Mapper, key any) (bool, error) {
	s, err := keyedShape(typeOf[T]())
	if err != nil {
		return false, err
	}
	if key, err = checkKey(s, key); err != nil {
		return false, err
	}
	result, err := m.Driver.ExecuteQuery("MATCH "+matchKey("n", s, "key")+" DETACH DELETE n RETURN count(*) AS deleted",
		map[string]any{"key": key})
	if err != nil {
		return false, err
	}
	return result.Records[0]["deleted"].(int64) > 0, nil
}

// Relate joins two nodes that are already stored with a relationship, or
// changes the one between them, and returns it as stored.
func Relate[A, R, B any](m *Mapper, from A, relationship R, to B) (R, error) {
	var zero R
	a, err := keyedShape(typeOf[A]())
	if err != nil {
		return zero, err
	}
	b, err := keyedShape(typeOf[B]())
	if err != nil {
		return zero, err
	}
	r, err := relationshipShape(typeOf[R]())
	if err != nil {
		return zero, err
	}
	fromKey, err := keyOf(a, from)
	if err != nil {
		return zero, err
	}
	toKey, err := keyOf(b, to)
	if err != nil {
		return zero, err
	}
	properties, err := PropertiesOf(relationship)
	if err != nil {
		return zero, err
	}
	result, err := m.Driver.ExecuteQuery(
		"MATCH "+matchKey("a", a, "a")+", "+matchKey("b", b, "b")+
			" MERGE (a)-[r:"+quoteName(r.name)+"]->(b) SET r += $properties RETURN r",
		map[string]any{"a": fromKey, "b": toKey, "properties": properties})
	if err != nil {
		return zero, err
	}
	if len(result.Records) == 0 {
		return zero, mappingError("%s or %s was not found to relate", a.goType.Name(), b.goType.Name())
	}
	return fromValue[R](result.Records[0]["r"])
}

// Pair is a relationship and the node at its far end.
type Pair[R, B any] struct {
	Relationship R
	Node         B
}

// Related returns the relationships of type R from a node to nodes of type B.
func Related[R, B, A any](m *Mapper, from A) ([]Pair[R, B], error) {
	a, err := keyedShape(typeOf[A]())
	if err != nil {
		return nil, err
	}
	b, err := nodeShape(typeOf[B]())
	if err != nil {
		return nil, err
	}
	r, err := relationshipShape(typeOf[R]())
	if err != nil {
		return nil, err
	}
	fromKey, err := keyOf(a, from)
	if err != nil {
		return nil, err
	}
	query := "MATCH " + matchKey("a", a, "a") + "-[r:" + quoteName(r.name) + "]->(b:" + quoteName(b.name) + ") RETURN r, b"
	if b.key != nil {
		query += " ORDER BY b." + quoteName(b.key.stored)
	}
	result, err := m.Driver.ExecuteQuery(query, map[string]any{"a": fromKey})
	if err != nil {
		return nil, err
	}
	out := make([]Pair[R, B], 0, len(result.Records))
	for _, record := range result.Records {
		relationship, err := fromValue[R](record["r"])
		if err != nil {
			return nil, err
		}
		node, err := fromValue[B](record["b"])
		if err != nil {
			return nil, err
		}
		out = append(out, Pair[R, B]{Relationship: relationship, Node: node})
	}
	return out, nil
}

// Query runs a statement and maps the first column of each row to a T: a
// node, a relationship, or a map of properties.
func Query[T any](m *Mapper, cypher string, parameters map[string]any) ([]T, error) {
	if _, err := shapeOf(typeOf[T]()); err != nil {
		return nil, err
	}
	return queryAs[T](m, cypher, parameters, "")
}

func queryAs[T any](m *Mapper, cypher string, parameters map[string]any, column string) ([]T, error) {
	result, err := m.Driver.ExecuteQuery(cypher, parameters)
	if err != nil {
		return nil, err
	}
	out := make([]T, 0, len(result.Records))
	if len(result.Keys) == 0 {
		return out, nil
	}
	if column == "" {
		column = result.Keys[0]
	}
	for _, record := range result.Records {
		object, err := fromValue[T](record[column])
		if err != nil {
			return nil, err
		}
		out = append(out, object)
	}
	return out, nil
}
