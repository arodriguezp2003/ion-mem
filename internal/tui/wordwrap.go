package tui

// wordwrap.go — Content word-wrapping for viewport rendering.
//
// bubbles/viewport renders lines as-is and clips at width; it does NOT wrap.
// wrapForViewport must be applied to every vp.SetContent and revVP.SetContent
// call so that long lines are broken before the viewport sees them.

import "strings"

// minWrapWidth is the minimum column width used for wrapping. Values ≤ 0 are
// clamped to this floor to avoid degenerate behaviour.
const minWrapWidth = 8

// wrapForViewport word-wraps content so that no output line exceeds width
// columns. It preserves existing newlines (multi-paragraph content is
// respected) and hard-breaks any single token whose length exceeds width
// (overlong URL, hash, or unbreakable word).
//
// Rules:
//  1. Each existing '\n' produces a new output line; the newline is NOT folded.
//  2. Within each input line, words are accumulated until the next word would
//     exceed width, at which point a newline is inserted.
//  3. A word longer than width is hard-broken at width boundaries.
//
// The function is O(n) in content length and allocates no regexp or external
// packages — this keeps it free of new module dependencies.
func wrapForViewport(content string, width int) string {
	if content == "" {
		return ""
	}
	if width < minWrapWidth {
		width = minWrapWidth
	}

	inputLines := strings.Split(content, "\n")
	// Pre-size the output builder to avoid repeated allocations.
	var out strings.Builder
	out.Grow(len(content) + len(content)/width*2)

	for lineIdx, line := range inputLines {
		if lineIdx > 0 {
			out.WriteByte('\n')
		}
		if line == "" {
			// Preserve blank lines as-is.
			continue
		}

		col := 0 // current column position in the output line being built
		words := strings.Fields(line)
		firstWord := true
		for _, word := range words {
			// Hard-break any token that is itself longer than width.
			// We consume it in chunks of up to (width - col) then width.
			for len(word) > 0 && (col == 0 && len(word) > width || col > 0 && col+1+len(word) > width && len(word) > width) {
				remaining := width - col
				if col > 0 {
					// Current line already has content; fit what we can, then break.
					if remaining <= 0 {
						out.WriteByte('\n')
						col = 0
						firstWord = true
						continue
					}
					// Need a space separator before the chunk when col > 0.
					out.WriteByte(' ')
					col++
					remaining = width - col
				}
				if remaining <= 0 {
					// Nothing fits; flush.
					out.WriteByte('\n')
					col = 0
					firstWord = true
					continue
				}
				chunk := word[:remaining]
				out.WriteString(chunk)
				col += len(chunk)
				word = word[len(chunk):]
				firstWord = false
				if len(word) > 0 {
					out.WriteByte('\n')
					col = 0
					firstWord = true
				}
			}
			if word == "" {
				continue
			}

			// Remaining word fits within width on its own (len(word) <= width).
			// Determine how many columns this word needs on the current line.
			need := len(word)
			if !firstWord {
				need++ // leading space
			}

			if col > 0 && col+need > width {
				// Word doesn't fit on the current line — start a new one.
				out.WriteByte('\n')
				col = 0
				firstWord = true
			}
			if !firstWord {
				out.WriteByte(' ')
				col++
			}
			out.WriteString(word)
			col += len(word)
			firstWord = false
		}
	}

	return out.String()
}
