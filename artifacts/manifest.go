// Package artifacts is the source of truth for feature-owned Lambda builds.
package artifacts

const Module = "github.com/lambdawalker/go.appexfission.aws.card-capture"

type Lambda struct{ Name, Package string }

// Lambdas returns independent archive names and their module command paths.
// Applications build these packages using their normal Go dependency graph.
func Lambdas() []Lambda {
	names := []string{"capture", "capture-worker", "capture-dispatcher"}
	out := make([]Lambda, 0, len(names))
	for _, name := range names {
		out = append(out, Lambda{Name: name, Package: Module + "/cmd/" + name})
	}
	return out
}
