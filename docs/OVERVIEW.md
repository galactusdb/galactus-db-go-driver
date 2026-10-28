# Galactus DB drivers

Seven native **database drivers**, with a shared API and zero third-party runtime
packages. Version **0.2.0** is early access. All seven source repositories are
public under [galactusdb](https://github.com/galactusdb), with `main` as the default
branch. Registry packages have not been published; install from source using the
language Quickstart and pin a reviewed commit. Each driver speaks **Bolt 5.4**
directly (accepting 5.0 to 5.4) and falls back to **Bolt 4.4** when the server
offers only that. Each uses its host language directly and needs neither a helper runtime nor
the server source at runtime.

Use **driver** for package names and public APIs. Use **SDK** for a future wider
bundle containing drivers, administration tools, examples, and integrations.

| Language | Folder | Public import / namespace | Runtime package dependencies |
|---|---|---|---|
| C# | [galactus-db-csharp-driver](https://github.com/galactusdb/galactus-db-csharp-driver) | `Galactus`; package `Galactus.Driver` | 0 |
| Rust | [galactus-db-rust-driver](https://github.com/galactusdb/galactus-db-rust-driver) | `galactus` | 0 |
| Java | [galactus-db-java-driver](https://github.com/galactusdb/galactus-db-java-driver) | `db.galactus` | 0 |
| Go | [galactus-db-go-driver](https://github.com/galactusdb/galactus-db-go-driver) | `galactus` | 0 |
| Node / TypeScript | [galactus-db-node-driver](https://github.com/galactusdb/galactus-db-node-driver) | `galactus-db-node-driver` | 0 |
| C++ | [galactus-db-cpp-driver](https://github.com/galactusdb/galactus-db-cpp-driver) | `galactus`; CMake `Galactus::Driver` | 0 |
| Python | [galactus-db-python-driver](https://github.com/galactusdb/galactus-db-python-driver) | `galactus` | 0 |

See the [driver catalogue](https://galactusdb.com/product/drivers) for repository
links and runtime requirements.

## Shared developer experience

1. Open a driver with a URI, username, password, and optional database/timeout.
2. Execute parameterised Cypher using `execute_query` / `executeQuery` /
   `ExecuteQuery`, following the language's conventions.
3. Read `records` by column name and retain `keys` and `summary` metadata.
4. Use `begin`, `commit`, and `rollback` for explicit transactions.
5. Close/dispose the driver; closing an open transaction rolls it back on the
   server. Rust and C++ also close on destruction.
6. Read the agreed protocol, server agent, connection id and latest bookmark
   from the driver (`ProtocolVersion()`, `ServerAgent()`, `ConnectionID()` and
   `LastBookmark()` in Go).

No generated models, ORM, schema registration, JSON conversion, or special
integer class is needed for ordinary values. Parameter values are encoded
recursively, and returned values are hydrated recursively, including spatial
values inside graph properties and nested containers.

Database integers are signed 64-bit. Node returns **native `bigint`**, while
JavaScript `number` parameters encode as floating point. Other languages use
their native signed integer representations. Out-of-range integers fail rather
than being truncated. Unknown wire structures remain explicit structures.

Nodes and relationships carry Bolt 5 element ids as strings (in Go `ElementID`,
`StartElementID` and `EndElementID`). On a Bolt 4.4 connection the driver fills
them from the integer id, which matches what Galactus returns over Bolt 5.
`DECIMAL` values arrive as strings holding their exact digits; send them back as
strings and convert with `decimal($param)`. Offset datetimes are written in the
Bolt 5 UTC form (`I`) on 5.x and the local form (`F`) on 4.4, and decoders accept
both on any version. Go's `ZonedDateTime` records whether its seconds are local
or UTC and is written in its own form on either protocol.

See [the blueprint and mapping table](BLUEPRINT.md) for exact semantics and
[the spatial contract](SPATIAL.md) for geometry/geography handling.

## All supported spatial types

Every driver has `Point2D`, `Point3D`, and a native `Spatial` value preserving
WKB bytes plus domain, SRID, XY/XYZ layout, and model. This covers Point,
LineString, Polygon with holes, MultiPoint, MultiLineString, MultiPolygon,
GeometryCollection, nested collections, and typed empties in both geometry and
geography.

The database's versioned envelope is used without coordinate rewriting or
conversion through GeoJSON. Bind a `Spatial` value directly, then use
`spatial.fromMap($shape)` in Cypher to turn that envelope into a database shape.
This explicit promotion is required by the existing server protocol.

## Scope of 0.2

These are small **single-connection, eager-result drivers**. One connection is
owned by each driver. Use a separate driver for each concurrent unit of work,
especially transactions. Go, Java and C# serialize calls; Node rejects overlapping
operations; Python and C++ must not be shared concurrently; Rust requires mutable
access. There is no connection pool, routing discovery, automatic retry,
managed transaction callback, lazy cursor, ORM, or async API outside Node in
0.2. Bookmarks are exposed but not sent; session re-authentication, routing and
telemetry messages are not used.

`bolt://` is direct TCP. `bolt+s://` is verified TLS with the URI host as the
server name. Python, Node, Go, Java and C# include it using their standard TLS
libraries (Go uses `crypto/tls`). Rust (`tls` cargo feature) and C++
(`GALACTUS_WITH_TLS`) offer it as an opt-in build so the default build keeps
zero dependencies. Every driver accepts an optional trusted-certificate PEM file
that replaces the platform store (Go: `Options.TrustedCertificates`); there is
no option to skip verification and no driver falls back to plaintext. A
Galactus server terminates TLS itself when started with `GDB_TLS_CERT` and
`GDB_TLS_KEY`, on the same port, and then requires TLS on that port.

Messages are limited to 64 MiB and nesting to 64 levels. Query results are eager
and may collectively exceed one message; use Cypher `LIMIT` for large queries.
I/O timeouts default to 30 seconds. They bound socket waits (Go uses a deadline
for the complete operation), not total DNS resolution or every query's total
elapsed time. After a database FAILURE the driver sends RESET, marks any
transaction as closed (the server rolls it back) and raises the error; the same
driver stays usable. Protocol, I/O, TLS and authentication failures, or a RESET
that does not succeed, discard the connection; open a new driver after those. A
FAILURE answering COMMIT means the transaction did not commit, while a transport
failure during COMMIT has an **unknown commit outcome**; the drivers never
automatically replay a write.

Database errors keep the server code and message and add the code's
classification, category and title, with client/transient/database checks and a
retryable flag that is true for transient errors only. Each driver publishes the
common codes as constants (Go: `galactus.CodeConstraintValidationFailed` and so on).

Returned graph entities are result-only database types; bind their IDs or
properties rather than the entities. Arbitrary application objects and UUIDs
are not implicitly converted; Python, Java and C# also accept their standard
decimal types. Convert them explicitly to a
supported representation. Use unique result aliases when reading record maps.

## Verification

[Test instructions](../tests/README.md) describe the shared golden wire fixtures,
malformed-input tests, and isolated real-server runner. Every language has been
compiled/executed locally against freshly built Galactus servers in three
passes: Bolt 5.4, a server capped at Bolt 4.4, and TLS with a throwaway CA. The common
spatial fixture has 57 cases per language, including both domains, XY/XYZ,
empties, holes, nested collections, and antimeridian geography.

Platform validation so far is Windows. Linux/macOS builds, pooling under load,
cancellation and production hardening remain release work.

## Further reading

See the [driver blueprint](BLUEPRINT.md) for the API and type mapping contract,
and [spatial mapping](SPATIAL.md) for geometry and geography interchange.
Visit [galactusdb.com](https://galactusdb.com) for the database and product guides.
