# Changelog

## 0.2.0 (28/10/2026)

- Speaks Bolt 5.4 (accepting 5.0 to 5.4) and falls back to Bolt 4.4. From 5.1
  the driver sends HELLO with `bolt_agent`, then authenticates with LOGON.
- `ProtocolVersion()`, `ServerAgent()`, `ConnectionID()` and `LastBookmark()`
  on the driver.
- Recovery after database errors: a FAILURE is followed by RESET, an open
  transaction is closed (the server rolls it back), and the same driver runs
  the next query. Connection, protocol, TLS and authentication failures still
  close the connection.
- `ConnectWithOptions` and `Options.TrustedCertificates`: a PEM file that
  replaces the system trust store for `bolt+s://`. Setting it on `bolt://` is a
  `*ConfigError`. Invalid URIs are also reported as `*ConfigError`.
- `DatabaseError` gains `Classification()`, `Category()`, `Title()`,
  `IsClientError()`, `IsTransient()`, `IsDatabaseError()` and `IsRetryable()`,
  and the package publishes code constants such as `ConstraintValidationFailed`.
- `Node`, `Relationship` and `UnboundRelationship` gain `ElementID` (and
  `StartElementID` / `EndElementID`), filled from the integer ids on Bolt 4.4.
- Datetimes: decoders accept `F`, `f`, `I` and `i` on any protocol. Offset
  datetimes are written as `I` on Bolt 5 and `F` on 4.4. `ZonedDateTime` gains
  a `UTC` basis field, `NewLocalZonedDateTime`, `NewUTCZonedDateTime`,
  `ZonedDateTimeFromTime` and `Time()`.
- `EncodeProtocol(value, bolt5)` encodes for a given protocol; `Encode` keeps
  the Bolt 4.4 forms.
- `DECIMAL` values are returned as strings holding their exact digits.
- A parameter that fails to encode no longer closes the connection.
- `user_agent` is now `galactus-db-go-driver/0.2.0`; `Version` and `UserAgent`
  constants are exported.

Compatibility: the 0.1 API keeps working. The graph and `ZonedDateTime` structs
have new trailing fields, so unkeyed composite literals of those types need
field names. A query after a database error now succeeds instead of reporting a
closed driver.

## 0.1.0

- First release: Bolt 4.4, parameterised queries, explicit transactions, native
  type mapping and spatial values.
