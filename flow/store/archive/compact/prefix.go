package compact

import (
	"fmt"
	"strings"
)

// ErrOutsidePrefix is returned when a key does not sit under the prefix
// its store was configured with. Callers check it with errors.Is; the
// operator sees which key was refused.
type ErrOutsidePrefix struct {
	Key    string
	Prefix string
}

func (e *ErrOutsidePrefix) Error() string {
	return fmt.Sprintf("key %q is not under prefix %q", e.Key, e.Prefix)
}

// checkUnderPrefix is the only thing standing between a bug in key
// construction and a delete somewhere else in the bucket. It runs on
// every operation, not only Delete, because a Write to the wrong place
// is how a later Delete finds the wrong thing to remove.
//
// The key is not cleaned, rewritten, or rejoined -- it is checked and
// then used exactly as given. Rebuilding a path from its parts is the
// mistake this whole area is being repaired for.
//
// A prefix boundary is a path boundary: "flows" covers "flows/a" and
// does not cover "flowsomething". Object stores have no directories, so
// nothing else enforces that.
//
// Dot segments are refused rather than resolved. Object stores treat
// them as ordinary characters, so "flows/../x" is a real key that no
// traversal occurs on -- but it reads as an escape, and any layer that
// ever normalizes it becomes one. Refusing costs nothing: nothing in
// this archive's layout produces such a key.
func checkUnderPrefix(key, prefix string) error {
	fail := func() error { return &ErrOutsidePrefix{Key: key, Prefix: prefix} }
	if key == "" || strings.HasPrefix(key, "/") {
		return fail()
	}
	for _, seg := range strings.Split(key, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return fail()
		}
	}
	prefix = strings.Trim(prefix, "/")
	if prefix == "" {
		return nil
	}
	if key != prefix && !strings.HasPrefix(key, prefix+"/") {
		return fail()
	}
	return nil
}
