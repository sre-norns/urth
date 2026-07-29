// Package integration exercises Urth's dispatch path across every boundary it
// crosses: Postgres, HTTP, JetStream, and the worker process.
//
// Everything else in this repository tests one of those and assumes the rest.
// That is the arrangement that hid the acknowledgement bug task 010 fixed --
// the worker acknowledged with Msg.Ack for months, and no test could see it
// because no test ever had a real broker and a real API at once. The suite here
// is the answer: ADR 0004's failure table, one scenario per row, against a real
// database, a real embedded broker, the real router, and the real worker loop.
//
// It has no non-test source of its own; this file exists so `go build ./...`
// has a package to look at. See harness_test.go for how a scenario is composed
// and what it costs to run one.
package integration
