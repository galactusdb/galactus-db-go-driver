package galactus

import "strings"

// DatabaseError is a FAILURE answered by the server. Code and Message are the
// server's own; Metadata is the complete failure map. After a failed query or
// transaction call the driver resets the connection and stays usable.
type DatabaseError struct {
	Code, Message string
	Metadata      map[string]any
}

func (e *DatabaseError) Error() string { return e.Code + ": " + e.Message }

func (e *DatabaseError) part(i int) string {
	parts := strings.Split(e.Code, ".")
	if len(parts) != 4 {
		return ""
	}
	return parts[i]
}

// Classification is ClientError, TransientError or DatabaseError ("" when the
// code does not have four parts).
func (e *DatabaseError) Classification() string { return e.part(1) }

// Category is the third part of the code, for example Schema or Statement.
func (e *DatabaseError) Category() string { return e.part(2) }

// Title is the last part of the code, for example ConstraintValidationFailed.
func (e *DatabaseError) Title() string { return e.part(3) }

func (e *DatabaseError) IsClientError() bool   { return e.Classification() == "ClientError" }
func (e *DatabaseError) IsTransient() bool     { return e.Classification() == "TransientError" }
func (e *DatabaseError) IsDatabaseError() bool { return e.Classification() == "DatabaseError" }

// IsRetryable is true for TransientError codes only. The driver never retries
// by itself.
func (e *DatabaseError) IsRetryable() bool { return e.IsTransient() }

// Published server codes. Codes the server adds later still arrive as plain
// strings in DatabaseError.Code.
const (
	ConstraintValidationFailed = "Galactus.ClientError.Schema.ConstraintValidationFailed"
	TypeError                  = "Galactus.ClientError.Statement.TypeError"
	ArgumentError              = "Galactus.ClientError.Statement.ArgumentError"
	SqlTooManyRows             = "Galactus.ClientError.Statement.SqlTooManyRows"
	SqlCastFailed              = "Galactus.ClientError.Statement.SqlCastFailed"
	TableTooLarge              = "Galactus.ClientError.Statement.TableTooLarge"
	UnsupportedFeature         = "Galactus.DatabaseError.Storage.UnsupportedFeature"
	ProcedureNotFound          = "Galactus.ClientError.Procedure.ProcedureNotFound"
	LicenseRestricted          = "Galactus.ClientError.License.Restricted"
	ReadOnlyReplica            = "Galactus.ClientError.Database.ReadOnlyReplica"
	TransactionTimedOut        = "Galactus.ClientError.Transaction.TimedOut"
	TransactionTerminated      = "Galactus.ClientError.Transaction.Terminated"
	SnapshotTooOld             = "Galactus.TransientError.Transaction.SnapshotTooOld"
	Forbidden                  = "Galactus.ClientError.Security.Forbidden"
	SyntaxError                = "Neo.ClientError.Statement.SyntaxError"
	SemanticError              = "Neo.ClientError.Statement.SemanticError"
	Unauthorized               = "Neo.ClientError.Security.Unauthorized"
	DatabaseNotFound           = "Neo.ClientError.Database.DatabaseNotFound"
)

// ConfigError reports an invalid URI or option. Nothing was sent.
type ConfigError struct{ Message string }

func (e *ConfigError) Error() string { return e.Message }

// encodeError marks a request that failed validation or encoding before any
// byte was written, so the connection is still in a known state.
type encodeError struct{ err error }

func (e *encodeError) Error() string { return e.err.Error() }
func (e *encodeError) Unwrap() error { return e.err }
