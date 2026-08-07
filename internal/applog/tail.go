package applog

import (
	"io"
	"os"
	"strings"
)

// tailBytes bounds how far back a tail reads. The log rotates well below this,
// so the last lines are always inside it, and a page that wants twenty lines
// never reads eighty kilobytes to find them.
const tailBytes = 256 << 10

// Tail returns the last lines of a log file, or an empty string when there is
// nothing to read. Errors are silence on purpose: every caller is showing this
// alongside something more important, and a page that fails because it could
// not decorate itself is worse than a page without the decoration.
func Tail(path string, lines int) string {
	file, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return ""
	}
	from := info.Size() - tailBytes
	if from < 0 {
		from = 0
	}
	if _, err := file.Seek(from, io.SeekStart); err != nil {
		return ""
	}
	raw, err := io.ReadAll(file)
	if err != nil {
		return ""
	}

	text := string(raw)
	// A read that started mid-file started mid-line. Drop the fragment rather
	// than show half a timestamp.
	if from > 0 {
		if _, rest, found := strings.Cut(text, "\n"); found {
			text = rest
		}
	}

	split := strings.Split(strings.TrimRight(text, "\n"), "\n")
	if len(split) > lines {
		split = split[len(split)-lines:]
	}
	return strings.Join(split, "\n")
}
