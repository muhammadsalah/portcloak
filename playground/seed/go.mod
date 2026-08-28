// The seeder is its own module on purpose.
//
// It is a tool for a playground, not part of PortCloak, and a nested module is
// invisible to `go build ./...` and `go test ./...` in the parent. Nothing here
// can break the application's build, and the application's dependencies do not
// grow to carry a generator of fake people.
//
// It has no dependencies at all. The Keycloak admin API is HTTP and JSON, LDAP
// entries are text, and both are in the standard library — a fixture generator
// that needs a dependency tree is a fixture generator that stops compiling
// eventually.
module portcloak/playground/seed

go 1.27
