package snapshot

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
)

// Algorithm is the digest used throughout. SHA-256 is ubiquitous and matches
// what S3 and Azure offer server-side, so a client-side digest can be
// cross-checked rather than merely trusted.
const Algorithm = "sha-256"

// ArtifactDigest is one leaf of the checksum tree.
type ArtifactDigest struct {
	Name   string `json:"name"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

// IntegrityTree is the per-artifact digests plus a root over them.
//
// The root is what makes tamper detection cheap; the leaves are what make a
// failure name the artifact that failed instead of declaring the whole bundle
// bad, which is the difference between a diagnosable problem and a dead end.
type IntegrityTree struct {
	Algorithm string           `json:"algorithm"`
	Root      string           `json:"root"`
	Artifacts []ArtifactDigest `json:"artifacts"`
}

// NewIntegrityTree builds the tree from a set of leaves.
func NewIntegrityTree(artifacts []ArtifactDigest) IntegrityTree {
	sorted := make([]ArtifactDigest, len(artifacts))
	copy(sorted, artifacts)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })

	h := sha256.New()
	for _, a := range sorted {
		// Name and digest are both folded in, so moving an artifact's contents
		// to a different name changes the root.
		fmt.Fprintf(h, "%s\x00%s\x00%d\n", a.Name, a.SHA256, a.Size)
	}
	return IntegrityTree{
		Algorithm: Algorithm,
		Root:      hex.EncodeToString(h.Sum(nil)),
		Artifacts: sorted,
	}
}

// Lookup finds one leaf.
func (t IntegrityTree) Lookup(name string) (ArtifactDigest, bool) {
	for _, a := range t.Artifacts {
		if a.Name == name {
			return a, true
		}
	}
	return ArtifactDigest{}, false
}

// ArtifactResult is the outcome of checking one artifact.
type ArtifactResult struct {
	Name     string `json:"name"`
	Expected string `json:"expected"`
	Actual   string `json:"actual"`
	OK       bool   `json:"ok"`
	// Note explains a result that is not a plain match — a missing artifact, or
	// one the tree does not mention.
	Note string `json:"note,omitempty"`
}

// VerifyResult is a whole-bundle verification, per artifact.
type VerifyResult struct {
	RootExpected string           `json:"rootExpected"`
	RootActual   string           `json:"rootActual"`
	OK           bool             `json:"ok"`
	Artifacts    []ArtifactResult `json:"artifacts"`
	Decryptable  bool             `json:"decryptable"`
	Message      string           `json:"message"`
}

// Failures lists the artifacts that did not verify.
func (v VerifyResult) Failures() []ArtifactResult {
	var out []ArtifactResult
	for _, a := range v.Artifacts {
		if !a.OK {
			out = append(out, a)
		}
	}
	return out
}

// Verify compares a set of observed digests against the tree.
func (t IntegrityTree) Verify(observed []ArtifactDigest) VerifyResult {
	seen := map[string]ArtifactDigest{}
	for _, o := range observed {
		seen[o.Name] = o
	}

	res := VerifyResult{RootExpected: t.Root, OK: true}
	for _, want := range t.Artifacts {
		got, present := seen[want.Name]
		switch {
		case !present:
			res.Artifacts = append(res.Artifacts, ArtifactResult{
				Name: want.Name, Expected: want.SHA256, OK: false,
				Note: "this artifact is listed in the snapshot but is not in the bundle",
			})
			res.OK = false
		case got.SHA256 != want.SHA256:
			res.Artifacts = append(res.Artifacts, ArtifactResult{
				Name: want.Name, Expected: want.SHA256, Actual: got.SHA256, OK: false,
				Note: "the contents do not match what was sealed",
			})
			res.OK = false
		default:
			res.Artifacts = append(res.Artifacts, ArtifactResult{
				Name: want.Name, Expected: want.SHA256, Actual: got.SHA256, OK: true,
			})
		}
		delete(seen, want.Name)
	}
	// An artifact the tree does not mention is a problem too: it is content
	// nobody sealed.
	for name, got := range seen {
		res.Artifacts = append(res.Artifacts, ArtifactResult{
			Name: name, Actual: got.SHA256, OK: false,
			Note: "the bundle holds an artifact that was not part of the sealed set",
		})
		res.OK = false
	}
	sort.Slice(res.Artifacts, func(i, j int) bool { return res.Artifacts[i].Name < res.Artifacts[j].Name })

	res.RootActual = NewIntegrityTree(observed).Root
	if res.OK && res.RootActual != res.RootExpected {
		// Belt and braces: the leaves agreed but the root did not, which means
		// the tree itself was edited.
		res.OK = false
		res.Message = "Every artifact matched individually, but the checksum tree's own root does not. The integrity record itself has been altered."
		return res
	}
	if res.OK {
		res.Message = fmt.Sprintf("All %d artifacts match what was sealed.", len(res.Artifacts))
	} else {
		names := make([]string, 0, 3)
		for _, f := range res.Failures() {
			if len(names) < 3 {
				names = append(names, f.Name)
			}
		}
		res.Message = fmt.Sprintf("%d of %d artifacts did not verify: %s.",
			len(res.Failures()), len(res.Artifacts), strings.Join(names, ", "))
	}
	return res
}
