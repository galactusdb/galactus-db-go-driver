# Galactus DB Go Driver

[Galactus DB](https://galactusdb.com) · [Driver catalogue](https://galactusdb.com/product/drivers) · [Type mapping](docs/BLUEPRINT.md) · [Spatial guide](docs/SPATIAL.md) · [Issues](https://github.com/galactusdb/galactus-db-go-driver/issues)

Connect Go applications to Galactus DB with parameterised Cypher,
native values, and explicit transactions. The driver speaks Bolt 5.4, falls
back to Bolt 4.4 when a server asks for it, verifies TLS against the server's
certificate, and has **zero third-party runtime package dependencies**: only
the Go standard library.

**Status: 0.2 / early access.** Source is available on `main`. Each driver owns
one connection and buffers results. After a query error the same driver keeps
working, and every database error tells you whether it is worth retrying.
Install from source as shown below and read the [scope](#scope) to see how it
fits your application.

## Contents

- [Quickstart](#quickstart)
- [Connection configuration](#connection-configuration)
- [Queries and results](#queries-and-results)
- [Transactions](#transactions)
- [Native type mapping](#native-type-mapping)
- [Spatial values](#spatial-values)
- [Errors and connection lifecycle](#errors-and-connection-lifecycle)
- [Testing](#testing)
- [Scope](#scope)

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

`ConnectWithOptions(uri, username, password, database, timeout, galactus.Options{...})`
takes the same arguments plus optional settings: `TrustedCertificates`,
described under [TLS](#tls).

| Setting | Behaviour |
|---|---|
| URI | Direct `bolt://host:port` or verified TLS `bolt+s://host:port`; omitted port is 7687 |
| Authentication | Required username and password; no credentials in the URI |
| Database | Empty/omitted database delegates selection to the server’s configured default |
| Named database | Supply an existing database name explicitly, for example `app` |
| Timeout | Default examples use 30 seconds; see the language-specific units above |
| Result handling | Records are buffered in memory until the query completes |

### TLS

Galactus DB serves TLS itself when it is started with `GDB_TLS_CERT` and
`GDB_TLS_KEY`, on the same port as plaintext. Connect with `bolt+s://`. The
driver uses Go's `crypto/tls`, checks the certificate chain and checks that the
certificate matches the host name in the URI.

Trust comes from the operating system's certificate store. For a private CA or
a self-signed development server, point the driver at a PEM file instead:

```go
driver, err := galactus.ConnectWithOptions("bolt+s://db.example.internal:7687", "gdb",
    os.Getenv("GDB_PASSWORD"), "", 30*time.Second,
    galactus.Options{TrustedCertificates: "/etc/galactus/ca.pem"})
```

The file may hold several certificates and replaces the system store for that
driver. Setting it with a `bolt://` URI returns a `*galactus.ConfigError`.
Verification is always on, and `bolt+s://` never falls back to plaintext.
`bolt+ssc://` and the `neo4j://` routing schemes are rejected.

### Connection details

After connecting, `driver.ProtocolVersion()` returns the agreed Bolt version
(`"5.4"`, or `"4.4"` when the server is capped with `GDB_BOLT_MAX_VERSION=4.4`).
`driver.ServerAgent()` and `driver.ConnectionID()` return what the server
reported, and `driver.LastBookmark()` returns the bookmark from the latest
autocommit query or commit. The driver keeps bookmarks for you to read; it does
not send them.

Timeouts bound socket waits, not necessarily total query duration or DNS lookup;
Go uses a deadline for the complete operation. Calls are serialized, but an explicit transaction belongs to the connection. Use one driver per concurrent unit of work. Calls do not take a `context.Context`; the timeout bounds each operation.

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
application-level pagination for large result sets: `ExecuteQuery` returns once
every record is in memory.

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

When the database rejects a query inside a transaction, the driver resets the
connection and the server rolls the transaction back. `Commit` then returns a
"no transaction" error, and the driver is ready for the next query or `Begin`.
A `*galactus.DatabaseError` from `Commit` means the transaction did not commit.
A lost connection during COMMIT is different: the outcome is unknown, so
reconcile the result before retrying a write.

## Native type mapping

Native `int64`, `float64`, `bool`, `string`, `[]byte`, `[]any`, and `map[string]any`. Smaller integer widths, checked unsigned integers, typed slices/maps, `time.Time`, and `time.Duration` work as inputs. Civil temporals and calendar durations use named structs.

- Database integers are signed 64-bit; out-of-range integer inputs fail.
- Bytes stay binary; values do not pass through JSON.
- Temporal wrappers preserve nanoseconds, offsets and named zones when the
  runtime’s native types cannot represent the full value.
- Calendar duration keeps months, days, seconds and nanoseconds separately.
- Graph nodes, relationships and paths have named result types. `Node`,
  `Relationship` and `UnboundRelationship` carry `ElementID` (and a
  relationship `StartElementID` / `EndElementID`) as strings; for Galactus these
  are the decimal text of the integer IDs on both Bolt 5 and Bolt 4.4. Bind IDs
  or properties as parameters; the graph entities themselves are result-only.
- Offset datetimes return as `time.Time` with a fixed zone of the original
  offset. Named-zone datetimes return as `ZonedDateTime`, which keeps the IANA
  zone name and records whether its seconds are local or UTC
  (`NewLocalZonedDateTime`, `NewUTCZonedDateTime`). `Time()` converts one with
  the system time zone database.
- `DECIMAL` values return as strings holding their exact digits, such as
  `"12.30"`. Send decimals as strings and convert them in Cypher with
  `decimal($amount)`.
- Unknown wire structures remain explicit structures and cannot be used as
  generic application-object serialization.

No ORM or automatic arbitrary-object mapping is included. Convert application
models and UUIDs explicitly to a supported value.
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

Server errors are `*galactus.DatabaseError` with `Code`, `Message` and
`Metadata` (use `errors.As`). The driver resets the connection after one, so the
same driver runs your next query. Each error also splits its code into
`Classification()`, `Category()` and `Title()`, and answers `IsClientError()`,
`IsTransient()`, `IsDatabaseError()` and `IsRetryable()`. Only transient errors
are retryable, and the driver never retries by itself.

Compare codes against the published constants:

```go
_, err := driver.ExecuteQuery("CREATE (:Account {email:$email})", map[string]any{"email": email})
var dbErr *galactus.DatabaseError
if errors.As(err, &dbErr) {
    switch {
    case dbErr.Code == galactus.CodeConstraintValidationFailed:
        // the email is already registered
    case dbErr.IsRetryable():
        // safe to run the transaction again
    }
}
```

The constants are `Code` followed by the title: `CodeConstraintValidationFailed`,
`CodeTypeError`, `CodeArgumentError`, `CodeSqlTooManyRows`, `CodeSqlCastFailed`,
`CodeTableTooLarge`, `CodeUnsupportedFeature`, `CodeProcedureNotFound`,
`CodeLicenseRestricted`, `CodeReadOnlyReplica`, `CodeTransactionTimedOut`,
`CodeTransactionTerminated`, `CodeSnapshotTooOld`, `CodeForbidden`,
`CodeSyntaxError`, `CodeSemanticError`, `CodeUnauthorized` and
`CodeDatabaseNotFound`. Codes the
server adds later still arrive in `Code` as plain strings.

Connection, protocol, TLS and timeout errors are ordinary Go errors; after one
of those the driver closes its connection. A rejected login also ends the
connection, because the server closes it. Invalid URIs and options are
`*galactus.ConfigError`.

| Symptom | Check |
|---|---|
| Connection refused | Host, port, listener and network reachability |
| Authentication rejected | Database username and configured password |
| Database not found | Explicit database name; omit it to use the server default |
| TLS negotiation fails | Server certificate, trust chain (`TrustedCertificates`) and certificate host name |
| Unsupported parameter | Convert the object to a supported scalar/container or named value |
| Driver is closed after an error | A connection, protocol or TLS failure closed it; open a new driver |

Credentials and parameters are not logged by the driver. Avoid writing them to
application logs when reporting errors. The driver performs no automatic write
retries and has no background connection pool to replace closed connections.

## Testing

Run from the repository root:

```sh
go test ./...
```

Fixtures and tests are self-contained. Without `GDB_TEST_URI`, live database
tests are skipped. For integration tests, use a fresh disposable server and
set `GDB_TEST_URI` and `GDB_TEST_PASSWORD`; tests create data and exercise
transactions. The configured default database is used. Reset the disposable
database before repeating the suite. `GDB_TEST_EXPECT_BOLT` checks the agreed
protocol, and `GDB_TEST_CA_FILE` supplies the trusted certificate for a
`bolt+s://` run.

See [test instructions](tests/README.md) for platform prerequisites and environment
examples. The shared live suite runs against Bolt 5.4, Bolt 4.4 and TLS servers
and covers 57 spatial cases, integer boundaries, native values, datetimes,
element ids, decimals, large chunked payloads, transactions, recovery after
errors and error classification.

## Scope

Version 0.2 provides direct connections over Bolt 5.4 or 4.4, verified TLS,
basic authentication, parameterised queries, buffered records and summaries,
explicit transactions, recovery after database errors, error classification,
native type mapping and full spatial interchange. Each driver owns a single
connection; connection pooling, cluster routing, managed retry callbacks,
streaming cursors and context cancellation are outside this release. Messages
are capped at 64 MiB and nesting at 64 levels; a buffered result can be larger
than a single message.

The driver has been tested on Windows against real Galactus servers speaking
Bolt 5.4, Bolt 4.4 and TLS. See [the shared blueprint](docs/BLUEPRINT.md) and
[implementation overview](docs/OVERVIEW.md) for the precise contract.

## Feedback and contribution

Use [GitHub issues](https://github.com/galactusdb/galactus-db-go-driver/issues) for reproducible
bugs and focused feature requests. Include runtime/OS versions, driver commit,
expected behaviour and a small reproduction with credentials and sensitive
data removed. Keep changes dependency-light and run the relevant tests before
opening a pull request against `main`.

Visit [galactusdb.com](https://galactusdb.com) for Galactus DB, or browse the
[other language drivers](https://galactusdb.com/product/drivers).

## Licence

Apache License 2.0. See [LICENSE](LICENSE).
