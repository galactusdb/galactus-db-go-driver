# Galactus Go driver

[Galactus DB website](https://galactusdb.com) · [Source](https://github.com/galactusdb/galactus-db-go-driver) · [Type mapping](docs/BLUEPRINT.md) · [Spatial types](docs/SPATIAL.md)

Native Bolt 4.4 driver for Galactus DB. This experimental 0.1 implementation
has zero third-party runtime package dependencies. Source is available here;
no npm, NuGet, PyPI, Maven Central, or crates.io release is implied.

## How To

### 1. Get the driver

```sh
git clone --branch main https://github.com/galactusdb/galactus-db-go-driver.git
cd galactus-db-go-driver
```

Go 1.21+; standard library only. In your application module:

```sh
go get github.com/galactusdb/galactus-db-go-driver@main
```

### 2. Configure your connection

Start or obtain a Galactus DB instance; visit [galactusdb.com](https://galactusdb.com)
for database information. Use its Bolt address (locally, `bolt://127.0.0.1:7687`),
username, and configured password. The example reads `GDB_PASSWORD` from your
environment; it is an application variable, not a command to change the server password.

```sh
# Bash / zsh
export GDB_PASSWORD='your-database-password'
```

```powershell
# PowerShell
$env:GDB_PASSWORD = 'your-database-password'
```

### 3. Execute a parameterised query

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
        os.Getenv("GDB_PASSWORD"), "neo4j", 30*time.Second)
    if err != nil { panic(err) }
    defer driver.Close()
    result, err := driver.ExecuteQuery("RETURN $name AS name", map[string]any{"name":"Ada"})
    if err != nil { panic(err) }
    fmt.Println(result.Records[0]["name"].(string))
}
```

An empty database selects `neo4j`; a nonpositive timeout selects 30 seconds.
`Begin(readOnly)`, `Commit()`, `Rollback()`, `Close()` manage lifecycle. Calls
are serialized, but transactions are connection-scoped: use a driver per
concurrent unit of work. Deadlines cover complete operations. No context API yet.
`bolt+s://` uses system trust with certificate/hostname verification.

Values decode to native primitives, `[]any`, and `map[string]any`. Typed slices
and string-keyed maps work as inputs. Integer output is int64; unsigned inputs
are range-checked. time.Time/time.Duration inputs are supported; calendar
durations, civil temporals, graph entities and spatial values use named structs.

See [mapping](docs/BLUEPRINT.md), [Spatial](docs/SPATIAL.md), and [scope](docs/OVERVIEW.md#scope-of-01).

### 4. Run the tests

From this repository's root:

```sh
go test ./...
```

See [test instructions](tests/README.md) for prerequisites and opt-in live tests.
Connection failures discard the connection; writes are never automatically retried.
Results are eager and each driver owns one connection; see the documented scope.

Learn more at [Galactus DB website](https://galactusdb.com).
