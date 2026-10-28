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
	CodeConstraintValidationFailed = "Galactus.ClientError.Schema.ConstraintValidationFailed"
	CodeTypeError                  = "Galactus.ClientError.Statement.TypeError"
	CodeArgumentError              = "Galactus.ClientError.Statement.ArgumentError"
	CodeSqlTooManyRows             = "Galactus.ClientError.Statement.SqlTooManyRows"
	CodeSqlCastFailed              = "Galactus.ClientError.Statement.SqlCastFailed"
	CodeTableTooLarge              = "Galactus.ClientError.Statement.TableTooLarge"
	CodeUnsupportedFeature         = "Galactus.DatabaseError.Storage.UnsupportedFeature"
	CodeProcedureNotFound          = "Galactus.ClientError.Procedure.ProcedureNotFound"
	CodeLicenseRestricted          = "Galactus.ClientError.License.Restricted"
	CodeReadOnlyReplica            = "Galactus.ClientError.Database.ReadOnlyReplica"
	CodeTransactionTimedOut        = "Galactus.ClientError.Transaction.TimedOut"
	CodeTransactionTerminated      = "Galactus.ClientError.Transaction.Terminated"
	CodeSnapshotTooOld             = "Galactus.TransientError.Transaction.SnapshotTooOld"
	CodeForbidden                  = "Galactus.ClientError.Security.Forbidden"
	CodeSyntaxError                = "Neo.ClientError.Statement.SyntaxError"
	CodeSemanticError              = "Neo.ClientError.Statement.SemanticError"
	CodeUnauthorized               = "Neo.ClientError.Security.Unauthorized"
	CodeDatabaseNotFound           = "Neo.ClientError.Database.DatabaseNotFound"
)

// ConfigError reports an invalid URI or option. Nothing was sent.
type ConfigError struct{ Message string }

func (e *ConfigError) Error() string { return e.Message }

// encodeError marks a request that failed validation or encoding before any
// byte was written, so the connection is still in a known state.
type encodeError struct{ err error }

func (e *encodeError) Error() string { return e.err.Error() }
func (e *encodeError) Unwrap() error { return e.err }
