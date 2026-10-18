// Package statements holds the tests over every hand-written statement
// (ADR 0013 §7): the conformance test, which prepares each registered
// sqlgen.Static against the migrated admin and workspace schemas, and the
// guard that keeps SQL from being written inline at a call site.
package statements
