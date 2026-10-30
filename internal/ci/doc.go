// Package ci holds checks on this repository's own build configuration.
//
// Nothing here ships in a binary. It exists because the pipeline definition
// is shell inside YAML, which no compiler reads, so the only place to assert
// anything about it is a test.
package ci
