// Package fault is the error catalog: every user-visible error has a Code with a
// registry entry that fixes its HTTP status, title, message, and recovery advice.
// Classification uses errors.Is/As and SQLite extended result codes, never message
// matching (message text is parsed only to extract a constraint name after the code
// is known).
package fault

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/ncruces/go-sqlite3"
)

// Code identifies an error kind. Codes are stable API.
type Code string

const (
	Unauthenticated        Code = "UNAUTHENTICATED"
	Forbidden              Code = "FORBIDDEN"
	NotFound               Code = "NOT_FOUND"
	BadRequest             Code = "BAD_REQUEST"
	ConcurrentModification Code = "CONCURRENT_MODIFICATION"
	IdempotencyConflict    Code = "IDEMPOTENCY_CONFLICT"
	DuplicateValue         Code = "DUPLICATE_VALUE"
	LinkedRecords          Code = "LINKED_RECORDS"
	ValidationFailed       Code = "VALIDATION_FAILED"
	RequiredField          Code = "REQUIRED_FIELD"
	SchemaChangeBlocked    Code = "SCHEMA_CHANGE_BLOCKED"
	WorkspaceBusy          Code = "WORKSPACE_BUSY"
	WorkspaceMaintenance   Code = "WORKSPACE_MAINTENANCE"
	QuotaExceeded          Code = "QUOTA_EXCEEDED"
	ScriptError            Code = "SCRIPT_ERROR"
	ScriptTimeout          Code = "SCRIPT_TIMEOUT"
	ScriptLimitExceeded    Code = "SCRIPT_LIMIT_EXCEEDED"
	SQLNotAllowed          Code = "SQL_NOT_ALLOWED"
	EgressDenied           Code = "EGRESS_DENIED"
	EgressFailed           Code = "EGRESS_FAILED"
	EgressResponseTooLarge Code = "EGRESS_RESPONSE_TOO_LARGE"
	ImportRowErrors        Code = "IMPORT_ROW_ERRORS"
	RestoreFailed          Code = "RESTORE_FAILED"
	BackupInvalid          Code = "BACKUP_INVALID"
	RateLimited            Code = "RATE_LIMITED"
	AppItemChanged         Code = "APP_ITEM_CHANGED"
	Internal               Code = "INTERNAL"
)

// Entry is the catalog definition of a Code.
type Entry struct {
	HTTPStatus    int
	Title         string
	Message       string // default message; callers may supply a more specific one
	Recovery      string
	Retryable     bool
	ShowTechnical bool // whether the cause text may be shown to builders
}

var registry = map[Code]Entry{
	Unauthenticated: {http.StatusUnauthorized, "Sign-in required", "You are not signed in, or your session has expired.", "Sign in and try again.", false, false},
	Forbidden:       {http.StatusForbidden, "Not allowed", "Your role does not permit this action.", "Ask a workspace owner for access.", false, false},
	NotFound:        {http.StatusNotFound, "Not found", "The requested item does not exist or you do not have access to it.", "Check the link, or refresh the list and try again.", false, false},
	BadRequest:      {http.StatusBadRequest, "Invalid request", "The request was not valid.", "Correct the highlighted values and try again.", false, true},
	ConcurrentModification: {http.StatusConflict, "Changed by someone else", "This record was changed by someone else after you opened it. Nothing was saved.",
		"Review the other changes, choose which values to keep, and save again.", false, false},
	IdempotencyConflict: {http.StatusConflict, "Request already used", "This request key was already used for a different request. Nothing was saved.", "Reload and try the action again.", false, false},
	DuplicateValue:      {http.StatusConflict, "Duplicate value", "Another record already has this value. Nothing was saved.", "Enter a different value.", false, false},
	LinkedRecords:       {http.StatusConflict, "Linked records", "This change would break a link between records. Nothing was saved.", "Remove or reassign the linked records first.", false, false},
	ValidationFailed:    {http.StatusUnprocessableEntity, "Validation failed", "A value does not satisfy the field's validation rule. Nothing was saved.", "Correct the value and try again.", false, false},
	RequiredField:       {http.StatusUnprocessableEntity, "Required field", "A required field is empty. Nothing was saved.", "Fill in the required field and try again.", false, false},
	SchemaChangeBlocked: {http.StatusConflict, "Schema change blocked", "This design change would break existing objects or data. Nothing was changed.",
		"Review the impact report. Fix the listed items, or confirm the change if you accept the impact.", false, false},
	WorkspaceBusy:        {http.StatusServiceUnavailable, "Workspace busy", "The workspace is busy with other changes. Nothing was saved.", "Wait a moment and try again.", true, false},
	WorkspaceMaintenance: {http.StatusServiceUnavailable, "Maintenance in progress", "The workspace is temporarily unavailable for maintenance.", "Try again shortly.", true, false},
	QuotaExceeded:        {http.StatusInsufficientStorage, "Quota exceeded", "The workspace has reached its storage or usage limit. Nothing was saved.", "Delete unneeded data or ask an administrator to raise the quota.", false, false},
	ScriptError:          {http.StatusUnprocessableEntity, "Script error", "A script raised an error.", "Check the script at the reported line.", false, true},
	ScriptTimeout:        {http.StatusUnprocessableEntity, "Script timed out", "A script ran longer than its time budget and was stopped.", "Make the script do less work, or move the work into a job.", false, true},
	ScriptLimitExceeded:  {http.StatusUnprocessableEntity, "Script limit exceeded", "A script exceeded a resource limit and was stopped.", "Reduce the size of strings or patterns the script builds.", false, true},
	SQLNotAllowed:        {http.StatusForbidden, "SQL not allowed", "The SQL statement uses an operation that is not permitted here.", "Use a read-only SELECT on workspace tables.", false, true},
	EgressDenied:         {http.StatusForbidden, "Outbound request blocked", "The destination is not on the platform's outbound allowlist or is not a public address.", "Ask a platform administrator to add the host to the egress allowlist.", false, true},
	EgressFailed:         {http.StatusBadGateway, "Outbound request failed", "The outbound request could not be completed.", "Check the destination service and try again.", true, true},
	EgressResponseTooLarge: {http.StatusBadGateway, "Response too large", "The outbound response exceeded the size limit and was discarded.",
		"Request less data, or raise max_bytes within the allowed maximum.", false, false},
	ImportRowErrors: {http.StatusUnprocessableEntity, "Import has row errors", "Some rows could not be imported.", "Review the listed rows, fix the source file or mapping, and try again.", false, false},
	RestoreFailed:   {http.StatusInternalServerError, "Restore failed", "The restore did not complete. The workspace was returned to its previous state.", "Check the server logs and try again.", false, false},
	BackupInvalid:   {http.StatusUnprocessableEntity, "Backup invalid", "The backup failed verification and cannot be restored.", "Choose a different backup.", false, false},
	AppItemChanged: {http.StatusServiceUnavailable, "Being updated", "This page is being updated.",
		"Try again later. If you manage this app, approve the changes in the app's settings.", true, false},
	RateLimited: {http.StatusTooManyRequests, "Too many requests", "Too many requests were made in a short time.", "Wait a moment and try again.", true, false},
	Internal:    {http.StatusInternalServerError, "Unexpected error", "An unexpected error occurred.", "Try again. If it keeps happening, contact an administrator with the request ID.", false, false},
}

// Lookup returns the registry entry for c, falling back to Internal.
func Lookup(c Code) Entry {
	if e, ok := registry[c]; ok {
		return e
	}
	return registry[Internal]
}

// Codes returns every registered code in sorted order (for generators and tests).
func Codes() []Code { return slices.Sorted(maps.Keys(registry)) }

// Error is a classified error carrying a Code.
type Error struct {
	Code       Code
	Message    string         // overrides the entry's default message when set
	Recovery   string         // overrides the entry's recovery advice when set
	Title      string         // overrides the entry's title when set
	Details    map[string]any // machine-readable context (e.g. details.current for OCC)
	RetryAfter time.Duration
	Cause      error
}

// GetHeaders carries Retry-After onto the response when an API handler
// returns the error (it satisfies huma.HeadersError).
func (e *Error) GetHeaders() http.Header {
	if e.RetryAfter <= 0 {
		return nil
	}
	secs := int(e.RetryAfter.Seconds() + 0.999)
	return http.Header{"Retry-After": []string{strconv.Itoa(max(secs, 1))}}
}

func (e *Error) Error() string {
	msg := e.Message
	if msg == "" {
		msg = Lookup(e.Code).Message
	}
	if e.Cause != nil {
		return fmt.Sprintf("%s: %s: %v", e.Code, msg, e.Cause)
	}
	return fmt.Sprintf("%s: %s", e.Code, msg)
}

func (e *Error) Unwrap() error { return e.Cause }

// Is matches another *Error by code, so errors.Is(err, fault.E(fault.NotFound)) works.
func (e *Error) Is(target error) bool {
	t, ok := target.(*Error)
	return ok && t.Code == e.Code && t.Message == "" && t.Cause == nil
}

// E returns a bare error for code (useful as an errors.Is target).
func E(code Code) *Error { return &Error{Code: code} }

// New returns an error with a specific message.
func New(code Code, msg string) *Error { return &Error{Code: code, Message: msg} }

// Newf formats a specific message.
func Newf(code Code, format string, args ...any) *Error {
	return &Error{Code: code, Message: fmt.Sprintf(format, args...)}
}

// SQLiteMessage is the part of a SQLite error that describes the problem ("no such column: x",
// "near \"SELEC\": syntax error"), without the leading result-code name ("SQL logic error: ").
func SQLiteMessage(se *sqlite3.Error) string {
	return strings.TrimPrefix(se.Error(), se.Code().Error()+": ")
}

// SQLError reports a statement SQLite rejected (syntax error, no such table). msg is SQLite's
// own text; the message is a sentence and the advice is about the statement, not form fields.
func SQLError(msg string, cause error) *Error {
	msg = strings.TrimSpace(msg)
	if !strings.HasSuffix(msg, ".") {
		msg += "."
	}
	return &Error{Code: BadRequest, Message: "SQL error: " + msg, Recovery: "Fix the statement and run it again.", Cause: cause}
}

// FileError reports an uploaded file that could not be read (malformed CSV, broken workbook). msg is
// a sentence that says where the problem is; recovery, when empty, is the generic advice for files.
func FileError(msg, recovery string, cause error) *Error {
	if recovery == "" {
		recovery = "Fix the file or choose a different one, then upload it again."
	}
	return &Error{Code: BadRequest, Title: "The file could not be read", Message: msg, Recovery: recovery, Cause: cause}
}

// Wrap attaches a code to cause.
func Wrap(code Code, cause error) *Error { return &Error{Code: code, Cause: cause} }

// WithDetails returns e with details set.
func (e *Error) WithDetails(d map[string]any) *Error { e.Details = d; return e }

// DisplayTitle is the short heading for the error.
func (e *Error) DisplayTitle() string {
	if e.Title != "" {
		return e.Title
	}
	return Lookup(e.Code).Title
}

// Entry returns e's registry entry.
func (e *Error) Entry() Entry { return Lookup(e.Code) }

// DisplayMessage is the user-facing message.
func (e *Error) DisplayMessage() string {
	if e.Message != "" {
		return e.Message
	}
	return Lookup(e.Code).Message
}

var checkNameRe = regexp.MustCompile(`CHECK constraint failed: ([A-Za-z0-9_]+)`)
var notNullRe = regexp.MustCompile(`NOT NULL constraint failed: [A-Za-z0-9_]+\.([A-Za-z0-9_]+)`)
var uniqueRe = regexp.MustCompile(`UNIQUE constraint failed: ([A-Za-z0-9_.]+(?:, [A-Za-z0-9_.]+)*)`)

// Classify maps any error to a *Error. Already-classified errors pass through.
// SQLite constraint failures carry the raw constraint or column in Details so the
// caller (which knows the catalog) can translate physical names to display names.
func Classify(err error) *Error {
	if err == nil {
		return nil
	}
	var fe *Error
	if errors.As(err, &fe) {
		return fe
	}
	// The driver reports most failures as *sqlite3.Error, but some paths (e.g.
	// SQLITE_FULL from a page-count limit during commit) return bare codes.
	var (
		code  sqlite3.ErrorCode
		ext   sqlite3.ExtendedErrorCode
		msg   string
		isSQL bool
	)
	var se *sqlite3.Error
	var sec sqlite3.ErrorCode
	var sext sqlite3.ExtendedErrorCode
	switch {
	case errors.As(err, &se):
		code, ext, msg, isSQL = se.Code(), se.ExtendedCode(), se.Error(), true
	case errors.As(err, &sext):
		code, ext, msg, isSQL = sqlite3.ErrorCode(sext), sext, sext.Error(), true
	case errors.As(err, &sec):
		code, ext, msg, isSQL = sec, sqlite3.ExtendedErrorCode(sec), sec.Error(), true
	}
	if isSQL {
		switch ext {
		case sqlite3.CONSTRAINT_UNIQUE, sqlite3.CONSTRAINT_PRIMARYKEY:
			d := map[string]any{}
			if m := uniqueRe.FindStringSubmatch(msg); m != nil {
				d["columns"] = m[1]
			}
			return &Error{Code: DuplicateValue, Details: d, Cause: err}
		case sqlite3.CONSTRAINT_FOREIGNKEY, sqlite3.CONSTRAINT_TRIGGER:
			// SQLite reports ON DELETE RESTRICT as CONSTRAINT_TRIGGER. Builders cannot
			// create triggers (the only trigger is the non-raising OCC safety net),
			// so this code always means a foreign key.
			return &Error{Code: LinkedRecords, Cause: err}
		case sqlite3.CONSTRAINT_CHECK:
			d := map[string]any{}
			if m := checkNameRe.FindStringSubmatch(msg); m != nil {
				d["constraint"] = m[1]
			}
			return &Error{Code: ValidationFailed, Details: d, Cause: err}
		case sqlite3.CONSTRAINT_NOTNULL:
			d := map[string]any{}
			if m := notNullRe.FindStringSubmatch(msg); m != nil {
				d["column"] = m[1]
			}
			return &Error{Code: RequiredField, Details: d, Cause: err}
		case sqlite3.CONSTRAINT_DATATYPE:
			return &Error{Code: ValidationFailed, Message: "A value has the wrong type for its field. Nothing was saved.", Cause: err}
		}
		switch code {
		case sqlite3.BUSY, sqlite3.LOCKED:
			return &Error{Code: WorkspaceBusy, Cause: err}
		case sqlite3.FULL:
			return &Error{Code: QuotaExceeded, Cause: err}
		case sqlite3.AUTH:
			return &Error{Code: SQLNotAllowed, Cause: err}
		case sqlite3.INTERRUPT:
			return &Error{Code: WorkspaceBusy, Message: "The operation was cancelled before it finished. Nothing was saved.", Cause: err}
		}
		return &Error{Code: Internal, Cause: err}
	}
	var pe *pgconn.PgError
	if errors.As(err, &pe) {
		return classifyPG(pe, err)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return &Error{Code: WorkspaceBusy, Message: "The operation took too long and was stopped.", Cause: err}
	}
	if errors.Is(err, context.Canceled) {
		return &Error{Code: WorkspaceBusy, Message: "The operation was cancelled.", Cause: err}
	}
	return &Error{Code: Internal, Cause: err}
}

// pgKeyRe reads the columns of a unique violation's detail, as in
// Key ("Email")=(a@example.com) already exists.
var pgKeyRe = regexp.MustCompile(`^Key \((.+?)\)=\(`)

// classifyPG maps a PostgreSQL error to the codes SQLite's map to, with the
// same details: the constraint, the column, or the columns.
func classifyPG(pe *pgconn.PgError, err error) *Error {
	switch pe.Code {
	case "23505": // unique_violation
		d := map[string]any{}
		if m := pgKeyRe.FindStringSubmatch(pe.Detail); m != nil {
			var cols []string
			for _, c := range strings.Split(m[1], ", ") {
				c, _, _ = strings.Cut(c, " COLLATE ")
				cols = append(cols, strings.ReplaceAll(strings.Trim(c, `"`), `""`, `"`))
			}
			d["columns"] = strings.Join(cols, ", ")
		}
		return &Error{Code: DuplicateValue, Details: d, Cause: err}
	case "23503", "23001": // foreign_key_violation, restrict_violation
		return &Error{Code: LinkedRecords, Cause: err}
	case "23514": // check_violation
		d := map[string]any{}
		if pe.ConstraintName != "" {
			d["constraint"] = pe.ConstraintName
		}
		return &Error{Code: ValidationFailed, Details: d, Cause: err}
	case "23502": // not_null_violation
		d := map[string]any{}
		if pe.ColumnName != "" {
			d["column"] = pe.ColumnName
		}
		return &Error{Code: RequiredField, Details: d, Cause: err}
	case "55P03", "40001", "40P01": // lock_not_available, serialization_failure, deadlock_detected
		return &Error{Code: WorkspaceBusy, Cause: err}
	case "57014": // query_canceled: statement_timeout or a cancelled context
		return &Error{Code: WorkspaceBusy, Message: "The operation was cancelled before it finished. Nothing was saved.", Cause: err}
	case "42501": // insufficient_privilege
		return &Error{Code: SQLNotAllowed, Cause: err}
	case "53100": // disk_full
		return &Error{Code: QuotaExceeded, Cause: err}
	}
	if strings.HasPrefix(pe.Code, "22") { // data_exception: a value that does not fit its column
		return &Error{Code: ValidationFailed, Message: "A value has the wrong type for its field. Nothing was saved.", Cause: err}
	}
	return &Error{Code: Internal, Cause: err}
}
