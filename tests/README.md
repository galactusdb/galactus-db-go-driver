# galactus-db-go-driver tests

[Galactus DB website](https://galactusdb.com) · [Driver Quickstart](../README.md)

## Quickstart

Run these commands from the repository root:

```sh
go vet ./...
go test ./...
```

The fixtures are included in `tests/fixtures`; no sibling repository is needed.
The tests use standard runtime/test facilities, with no test framework packages.

- `values.tsv`: integer boundaries, UTF-8, bytes, temporal values, graph values,
  points and unknown structures. Each row decodes and re-encodes to the same
  bytes in Bolt 4.4 mode.
- `bolt5.tsv`: Bolt 5 graph values with element ids and the `I`/`i`/`f`
  datetime forms. Each row round-trips byte for byte in Bolt 5 mode.
- `datetime_pairs.tsv`: the same offset datetime as `F` (column 1) and `I`
  (column 2). Both decode to equal values; encoding gives column 1 in 4.4 mode
  and column 2 in Bolt 5 mode.
- `malformed.tsv`: truncation, invalid markers/UTF-8, huge lengths, bad and
  duplicate keys, trailing data, malformed structures and depth bombs.
- `spatial.tsv`: 57 geometry/geography cases, including XY/XYZ, empties, holes,
  multipart shapes, nested collections, and antimeridian geography.

Unit tests also cover error classification and configuration errors (unsupported
schemes, URI options and a trusted-certificate file on `bolt://`).

### Live database tests

Without `GDB_TEST_URI`, live tests are skipped. To include them, start a **fresh,
disposable** Galactus database and set these environment variables before running
the same test command:

```sh
export GDB_TEST_URI='bolt://127.0.0.1:7687'
export GDB_TEST_PASSWORD='your-disposable-database-password'
export GDB_TEST_EXPECT_BOLT='5.4'           # optional: check the agreed protocol
export GDB_TEST_CA_FILE='/path/to/ca.pem'   # bolt+s:// runs only
```

```powershell
$env:GDB_TEST_URI = 'bolt://127.0.0.1:7687'
$env:GDB_TEST_PASSWORD = 'your-disposable-database-password'
$env:GDB_TEST_EXPECT_BOLT = '5.4'
```

Tests use username `gdb` and the server's default database. They check the
agreed protocol, native values and large chunked strings, spatial round trips,
offset and named-zone (`Europe/London`) datetimes, element ids on nodes and
relationships, commit and rollback, recovery after a failed query (the same
driver then runs `RETURN 1`), a failure inside a transaction closing it, a
unique-constraint violation reported as `ConstraintValidationFailed` with its
classification, `DECIMAL` results as strings, and rejected authentication. When
`GDB_TEST_CA_FILE` is set the URI must be `bolt+s://`: the test connects with the
trusted certificate, checks that connecting without it fails certificate
verification, and that the option on a `bolt://` URI is a configuration error.

Do not point them at valuable data. Reset the disposable database between runs
because the tests leave nodes and a constraint. The shared runner in the drivers
workspace (`tests/run_live.py`) starts disposable servers and runs the suite in
three passes: Bolt 5.4, a server capped with `GDB_BOLT_MAX_VERSION=4.4`, and TLS.
See [implementation scope](../docs/OVERVIEW.md#scope-of-02).

Database information: [Galactus DB website](https://galactusdb.com).
