// Package diff produces unified diffs of text files, in the format
// written by "diff -u".
package diff

import (
	"bytes"
	"fmt"
	"strings"
)

// Context is the number of unchanged lines shown around each change.
const Context = 3

// maxEdits bounds the work the diff algorithm will do. Beyond it the
// files are reported as wholly replaced, which is both accurate and
// cheap.
const maxEdits = 10000

// Unified returns a unified diff of a and b, labelled with the given
// names. It returns the empty string when the contents are identical.
func Unified(aname, bname string, a, b []byte) string {
	if bytes.Equal(a, b) {
		return ""
	}
	if isBinary(a) || isBinary(b) {
		return fmt.Sprintf("Binary files %s and %s differ\n", aname, bname)
	}
	alines, aeol := splitLines(a)
	blines, beol := splitLines(b)

	var out strings.Builder
	fmt.Fprintf(&out, "--- %s\n", aname)
	fmt.Fprintf(&out, "+++ %s\n", bname)
	for _, h := range hunks(script(alines, blines)) {
		fmt.Fprintf(&out, "@@ -%s +%s @@\n", span(h.astart, h.acount), span(h.bstart, h.bcount))
		for _, e := range h.edits {
			switch e.op {
			case opEqual:
				writeLine(&out, ' ', alines[e.a], e.a == len(alines)-1 && !aeol)
			case opDelete:
				writeLine(&out, '-', alines[e.a], e.a == len(alines)-1 && !aeol)
			case opInsert:
				writeLine(&out, '+', blines[e.b], e.b == len(blines)-1 && !beol)
			}
		}
	}
	return out.String()
}

func writeLine(out *strings.Builder, prefix byte, line string, noEOL bool) {
	out.WriteByte(prefix)
	out.WriteString(line)
	out.WriteByte('\n')
	if noEOL {
		out.WriteString("\\ No newline at end of file\n")
	}
}

// span formats a hunk range the way diff does, eliding the count when it
// is one and the start when the range is empty.
func span(start, count int) string {
	if count == 0 {
		return fmt.Sprintf("%d,0", start)
	}
	if count == 1 {
		return fmt.Sprintf("%d", start+1)
	}
	return fmt.Sprintf("%d,%d", start+1, count)
}

func isBinary(data []byte) bool {
	if len(data) > 8000 {
		data = data[:8000]
	}
	return bytes.IndexByte(data, 0) >= 0
}

// splitLines splits data into lines, dropping the line terminators. It
// also reports whether the data ended with a newline.
func splitLines(data []byte) (lines []string, eol bool) {
	if len(data) == 0 {
		return nil, true
	}
	s := string(data)
	eol = strings.HasSuffix(s, "\n")
	if eol {
		s = s[:len(s)-1]
	}
	return strings.Split(s, "\n"), eol
}

type opcode byte

const (
	opEqual opcode = iota
	opDelete
	opInsert
)

// An edit is a single line of the edit script: a and b index the two
// line slices, and only the index relevant to op is meaningful.
type edit struct {
	op   opcode
	a, b int
}

// script returns the edit script transforming a into b. It strips the
// common prefix and suffix before running the O(ND) search, which keeps
// the usual case of a small change to a large file cheap.
func script(a, b []string) []edit {
	var pre int
	for pre < len(a) && pre < len(b) && a[pre] == b[pre] {
		pre++
	}
	var suf int
	for suf < len(a)-pre && suf < len(b)-pre && a[len(a)-1-suf] == b[len(b)-1-suf] {
		suf++
	}
	mid := myers(a[pre:len(a)-suf], b[pre:len(b)-suf])

	edits := make([]edit, 0, pre+len(mid)+suf)
	for i := 0; i < pre; i++ {
		edits = append(edits, edit{opEqual, i, i})
	}
	for _, e := range mid {
		e.a += pre
		e.b += pre
		edits = append(edits, e)
	}
	for i := 0; i < suf; i++ {
		edits = append(edits, edit{opEqual, len(a) - suf + i, len(b) - suf + i})
	}
	return edits
}

// myers computes a shortest edit script by Myers's algorithm, recording
// the search's frontier at each step so that the path can be recovered.
func myers(a, b []string) []edit {
	n, m := len(a), len(b)
	if n == 0 && m == 0 {
		return nil
	}
	if max := n + m; max > maxEdits {
		return replaceAll(n, m)
	}
	max := n + m
	offset := max
	v := make([]int, 2*max+1)
	trace := make([][]int, 0, max+1)
	for d := 0; d <= max; d++ {
		trace = append(trace, append([]int(nil), v...))
		for k := -d; k <= d; k += 2 {
			var x int
			if k == -d || (k != d && v[offset+k-1] < v[offset+k+1]) {
				x = v[offset+k+1]
			} else {
				x = v[offset+k-1] + 1
			}
			y := x - k
			for x < n && y < m && a[x] == b[y] {
				x++
				y++
			}
			v[offset+k] = x
			if x >= n && y >= m {
				return backtrack(trace, n, m, offset)
			}
		}
	}
	return replaceAll(n, m) // unreachable
}

// replaceAll is the trivial edit script: delete all of a, insert all of
// b.
func replaceAll(n, m int) []edit {
	edits := make([]edit, 0, n+m)
	for i := 0; i < n; i++ {
		edits = append(edits, edit{opDelete, i, 0})
	}
	for j := 0; j < m; j++ {
		edits = append(edits, edit{opInsert, 0, j})
	}
	return edits
}

// backtrack walks the recorded frontiers from the end of both inputs
// back to the start, emitting the edits it passes through.
func backtrack(trace [][]int, n, m, offset int) []edit {
	var edits []edit
	x, y := n, m
	for d := len(trace) - 1; d >= 0; d-- {
		v := trace[d]
		k := x - y
		var prevX, prevY int
		if d > 0 {
			var prevK int
			if k == -d || (k != d && v[offset+k-1] < v[offset+k+1]) {
				prevK = k + 1
			} else {
				prevK = k - 1
			}
			prevX = v[offset+prevK]
			prevY = prevX - prevK
		}
		for x > prevX && y > prevY {
			x--
			y--
			edits = append(edits, edit{opEqual, x, y})
		}
		if d > 0 {
			if x == prevX {
				y--
				edits = append(edits, edit{opInsert, x, y})
			} else {
				x--
				edits = append(edits, edit{opDelete, x, y})
			}
		}
		x, y = prevX, prevY
	}
	for i, j := 0, len(edits)-1; i < j; i, j = i+1, j-1 {
		edits[i], edits[j] = edits[j], edits[i]
	}
	return edits
}

// A hunk is a run of edits with its surrounding context.
type hunk struct {
	astart, acount int
	bstart, bcount int
	edits          []edit
}

// hunks groups an edit script into the hunks a unified diff prints,
// merging runs of changes that lie within twice the context of one
// another.
func hunks(edits []edit) []hunk {
	var hs []hunk
	for i := 0; i < len(edits); {
		if edits[i].op == opEqual {
			i++
			continue
		}
		// Find the end of this run of changes, absorbing short spans
		// of equal lines that would otherwise appear as context twice.
		start := i
		end := i
		for j := i; j < len(edits); j++ {
			if edits[j].op != opEqual {
				end = j + 1
				continue
			}
			if j-end >= 2*Context {
				break
			}
		}
		lo := max(0, start-Context)
		hi := min(len(edits), end+Context)
		hs = append(hs, newHunk(edits[lo:hi]))
		i = hi
	}
	return hs
}

// newHunk computes the line ranges covered by a slice of the edit
// script.
func newHunk(edits []edit) hunk {
	h := hunk{edits: edits}
	h.astart, h.bstart = -1, -1
	for _, e := range edits {
		switch e.op {
		case opEqual:
			if h.astart < 0 {
				h.astart, h.bstart = e.a, e.b
			}
			h.acount++
			h.bcount++
		case opDelete:
			if h.astart < 0 {
				h.astart, h.bstart = e.a, e.b
			}
			h.acount++
		case opInsert:
			if h.astart < 0 {
				h.astart, h.bstart = e.a, e.b
			}
			h.bcount++
		}
	}
	if h.astart < 0 {
		h.astart, h.bstart = 0, 0
	}
	return h
}
