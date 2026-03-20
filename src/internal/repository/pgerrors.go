package repository

// PostgreSQL error codes for constraint violations.
// See: https://www.postgresql.org/docs/current/errcodes-appendix.html
const (
	PgErrCodeUniqueViolation     = "23505" // unique_violation
	PgErrCodeForeignKeyViolation = "23503" // foreign_key_violation
	PgErrCodeCheckViolation      = "23514" // check_violation
)
