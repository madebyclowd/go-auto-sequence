// Package integration holds tests that need a real database. It is a separate module so the
// root module never depends on a driver. Tests skip unless SEQ_TEST_POSTGRES_DSN is set.
package integration
