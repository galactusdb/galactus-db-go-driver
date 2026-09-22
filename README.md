# Galactus DB Go Driver

[Galactus DB](https://galactusdb.com) · [Driver catalogue](https://galactusdb.com/product/drivers) · [Type mapping](docs/BLUEPRINT.md) · [Spatial guide](docs/SPATIAL.md) · [Issues](https://github.com/galactusdb/galactus-db-go-driver/issues)

Connect Go applications to Galactus DB with parameterised Cypher,
native values, and explicit transactions. The driver speaks Bolt 4.4 directly
and has **zero third-party runtime package dependencies**.

**Status: 0.1 / early access.** Source is available on `main`. This is a working,
tested baseline with one connection per driver and eager results. Package-registry
releases, pooling and cancellation are not available yet. Use source installs
below and evaluate the [current scope](#current-scope) for your application.

## Contents

- [Quickstart](#quickstart)
- [Connection configuration](#connection-configuration)
- [Queries and results](#queries-and-results)
- [Transactions](#transactions)
- [Native type mapping](#native-type-mapping)
- [Spatial values](#spatial-values)
- [Errors and connection lifecycle](#errors-and-connection-lifecycle)
- [Testing](#testing)
- [Current scope](#current-scope)

## Quickstart

### 1. Get the source

Requirements: **Go 1.21+**, Git, and a reachable Galactus DB instance.

```sh
git clone --branch main https://github.com/galactusdb/galactus-db-go-driver.git
cd galactus-db-go-driver
```

Run this in your application module.

```sh
go get github.com/galactusdb/galactus-db-go-driver@main
```

Git-based installs follow `main`; pin a reviewed commit in your application for
reproducible builds. No npm, NuGet, PyPI, Maven Central or crates.io availability
is implied by the package names.

### 2. Configure your database credentials

Start Galactus DB on your infrastructure using the setup available from
[galactusdb.com](https://galactusdb.com). The examples use
`bolt://127.0.0.1:7687` and username `gdb`; replace them with your server settings.
Set your configured password in the application environment:

```sh
# Bash / zsh
export GDB_PASSWORD='your-database-password'
```

```powershell
# PowerShell
$env:GDB_PASSWORD = 'your-database-password'
```

`GDB_PASSWORD` is read by these examples; the driver does not automatically load
environment variables or change the server password. Keep credentials out of
source control and connection URIs.

### 3. Run your first query

```go
package main

import (
    "fmt"
    "os"
    "time"
    galactus "github.com/galactusdb/galactus-db-go-driver"
)

func main() {
    driver, err := galactus.Connect("bolt://127.0.0.1:7687", "gdb",
        os.Getenv("GDB_PASSWORD"), "", 30*time.Second)
    if err != nil { panic(err) }
    defer driver.Close()
    result, err := driver.ExecuteQuery("RETURN $name AS name", map[string]any{"name":"Ada"})
    if err != nil { panic(err) }
    fmt.Println(result.Records[0]["name"].(string))
}
```

The following examples reuse an open `driver` within the same resource scope.
Adapt error handling to your application.

## Connection configuration

`Connect(uri, username, password, database, timeout)`: pass `""` for the server default and `30*time.Second` for a 30-second operation deadline. A nonpositive timeout selects 30 seconds. Close with `defer driver.Close()`.

| Setting | Behaviour |
|---|---|
| URI | Direct `bolt://host:port`; omitted port is 7687 |
| Authentication | Required username and password; no credentials in the URI |
| Database | Empty/omitted database delegates selection to the server’s configured default |
| Named database | Supply an existing database name explicitly, for example `app` |
| Timeout | Default examples use 30 seconds; see the language-specific units above |
| Result handling | Records are buffered in memory until the query completes |

Python, Node, Go, Java and C# support verified `bolt+s://` using their system TLS facilities. This driver validates certificate trust and hostname; use a TLS terminator when the database listener is plaintext.

Timeouts bound socket waits, not necessarily total query duration or DNS lookup;
Go uses a deadline for the complete operation. Calls are serialized, but an explicit transaction belongs to the connection. Use one driver per concurrent unit of work. Context cancellation is not implemented yet.

## Queries and results

Bind data as parameters, never interpolate user input into Cypher:

```go
result, err := driver.ExecuteQuery(
    "CREATE (p:Person {name:$name, age:$age}) RETURN p.name AS name, p.age AS age",
    map[string]any{"name": "Ada", "age": int64(37)},
)
if err != nil { panic(err) }
fmt.Println(result.Records[0]["name"].(string))
fmt.Println(result.Summary)
```

Results expose column keys, records indexed by column name, and server summary
metadata. Read a column with `result.Records[0]["name"]`. Use unique column aliases because
records are maps. Summary fields can include the database, query classification,
timings, bookmark and update statistics; only fields supplied by the server are
present. A query without rows still returns its summary.

Lists and maps can be nested. Values are recursively encoded and hydrated,
including graph properties and spatial envelopes. Use Cypher `LIMIT` or
application-level pagination for large result sets: this release is eager,
not a lazy streaming cursor.

## Transactions

Queries outside an explicit transaction autocommit. Group related writes with
begin/commit; explicitly roll back when abandoning work:

```go
if err := driver.Begin(false); err != nil { panic(err) }
if _, err := driver.ExecuteQuery("CREATE (:Person {name:$name})", map[string]any{"name":"Ada"}); err != nil { panic(err) }
if _, err := driver.ExecuteQuery("CREATE (:Person {name:$name})", map[string]any{"name":"Grace"}); err != nil { panic(err) }
if _, err := driver.Commit(); err != nil { panic(err) }
// To deliberately abandon an open transaction: driver.Rollback()
```

The transaction belongs to the connection. Keep both statements on the same
driver and finish the transaction before reusing it for unrelated work. Closing
or disposing a connection rolls back an unfinished transaction; it never
implicitly commits. The read-only argument requests a read transaction.

If a query or transport failure occurs, the driver discards the connection.
Do not issue rollback on a connection already discarded after failure. A lost
connection during COMMIT can mean the commit outcome is unknown; reconcile the
result before retrying a write.

## Native type mapping

Native `int64`, `float64`, `bool`, `string`, `[]byte`, `[]any`, and `map[string]any`. Smaller integer widths, checked unsigned integers, typed slices/maps, `time.Time`, and `time.Duration` work as inputs. Civil temporals and calendar durations use named structs.

- Database integers are signed 64-bit; out-of-range integer inputs fail.
- Bytes stay binary; values do not pass through JSON.
- Temporal wrappers preserve nanoseconds, offsets and named zones when the
  runtime’s native types cannot represent the full value.
- Calendar duration keeps months, days, seconds and nanoseconds separately.
- Graph nodes, relationships and paths have named result types. Bind their IDs
  or properties as parameters; the graph entities themselves are result-only.
- Unknown wire structures remain explicit structures and cannot be used as
  generic application-object serialization.

No ORM or automatic arbitrary-object mapping is included. Convert application
models, UUIDs and decimal values explicitly to your chosen supported format.
See the [complete type mapping](docs/BLUEPRINT.md) for precision and range details.

## Spatial values

The driver supports `Point2D`, `Point3D`, and a native `Spatial` value covering
every shape family currently supported by Galactus DB:

| Shape family | Geometry | Geography |
|---|---|---|
| Point / LineString / Polygon, including holes | Yes | Yes |
| MultiPoint / MultiLineString / MultiPolygon | Yes | Yes |
| GeometryCollection, including nested collections | Yes | Yes |
| XY, XYZ, and typed empty shapes | Yes | Yes |

`Spatial` preserves WKB bytes together with domain, SRID, coordinate layout and
model. Geometry uses `planar`; geography uses `greatCircle`. Neither domain nor
SRID is inferred by converting through GeoJSON.

```go
result, err := driver.ExecuteQuery(
    "RETURN spatial.fromWKT($wkt, {domain:$domain}) AS shape",
    map[string]any{"wkt":"POLYGON((0 0,4 0,4 4,0 4,0 0))", "domain":"geometry"},
)
if err != nil { panic(err) }
shape := result.Records[0]["shape"].(galactus.Spatial)
_, err = driver.ExecuteQuery(
    "CREATE (:Region {name:$name, shape:spatial.fromMap($shape)})",
    map[string]any{"name":"Square", "shape":shape},
)
if err != nil { panic(err) }
fmt.Println(shape.Domain, shape.SRID, shape.Layout, len(shape.WKB))
```

Binding `Spatial` sends a lossless envelope. Use `spatial.fromMap($shape)` to
promote it into a database spatial value. `RETURN $shape` alone echoes the map;
it does not change the server’s value type. To import external bytes or text,
use `spatial.fromWKB`, `spatial.fromWKT`, or `spatial.fromGeoJSON` with parameters.
The server performs geometry/topology validation. M/ZM, curves, surfaces and
solids are outside the current database scope. See the [spatial guide](docs/SPATIAL.md).

## Errors and connection lifecycle

Server errors expose `*galactus.DatabaseError` with `Code`, `Message`, and `Metadata` (use `errors.As`). Distinguish these from protocol, connection,
timeout, and unsupported-value errors in your application.

| Symptom | Check |
|---|---|
| Connection refused | Host, port, listener and network reachability |
| Authentication rejected | Database username and configured password |
| Database not found | Explicit database name; omit it to use the server default |
| TLS negotiation fails | TLS terminator, trust chain and certificate hostname |
| Unsupported parameter | Convert the object to a supported scalar/container or named value |
| Driver is closed after an error | Open a new connection; failed connections are discarded |

Credentials and parameters are not logged by the driver. Avoid writing them to
application logs when reporting errors. The driver performs no automatic write
retries and has no background connection pool to recover failed connections.

## Testing

Run from the repository root:

```sh
go test ./...
```

Fixtures and tests are self-contained. Without `GDB_TEST_URI`, live database
tests are skipped. For integration tests, use a fresh disposable server and
set `GDB_TEST_URI` and `GDB_TEST_PASSWORD`; tests create data and exercise
transactions. The configured default database is used. Reset the disposable
database before repeating the suite.

See [test instructions](tests/README.md) for platform prerequisites and environment
examples. The shared live suite covers 57 spatial cases per language, integer
boundaries, native values, large chunked payloads, transactions and failures.

## Current scope

This release provides direct connections, basic authentication, parameterised
queries, eager records and summaries, explicit transactions, native type mapping,
and full supported spatial interchange. It does not yet provide pooling,
routing discovery, managed retry callbacks, lazy cursors, cancellation, an ORM,
or asynchronous APIs outside Node. Messages are capped at 64 MiB and nesting at
64 levels; aggregate result memory can exceed a single message.

Windows builds and real-server interoperability have been tested. Broader
platform validation, TLS interoperability and production load testing remain
release work. See [the shared blueprint](docs/BLUEPRINT.md) and
[implementation overview](docs/OVERVIEW.md) for the precise contract.

## Feedback and contribution

Use [GitHub issues](https://github.com/galactusdb/galactus-db-go-driver/issues) for reproducible
bugs and focused feature requests. Include runtime/OS versions, driver commit,
expected behaviour and a small reproduction with credentials and sensitive
data removed. Keep changes dependency-light and run the relevant tests before
opening a pull request against `main`.

Visit [galactusdb.com](https://galactusdb.com) for Galactus DB, or browse the
[other language drivers](https://galactusdb.com/product/drivers).
