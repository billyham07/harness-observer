package source

import (
	"bufio"
	"errors"
	"io"
	"os"
)

// MaxLineBytes caps a single JSONL record. Session transcripts can contain very
// large tool payloads; anything bigger is not buffered into memory.
const MaxLineBytes = 32 << 20

// ScanLines streams path from state.Offset and calls fn for every line, without
// ever loading the whole file.
//
// A trailing line with no newline is a record the harness may still be writing.
// It is handed to fn (a half-written JSON object simply fails to parse, so no
// partial evidence can leak in) but the returned offset stops *before* it, so
// the next scan re-reads it once the writer finished. Re-reading is safe because
// record IDs are stable and persistence is idempotent.
func ScanLines(path string, state FileState, fn func(line []byte, lineNo int) error) (FileState, error) {
	f, err := os.Open(path)
	if err != nil {
		return state, err
	}
	defer f.Close()

	if state.Offset > 0 {
		if _, err := f.Seek(state.Offset, io.SeekStart); err != nil {
			return state, err
		}
	}

	next := state
	r := bufio.NewReaderSize(f, 256<<10)
	for {
		line, complete, readErr := readLine(r)
		if len(line) > 0 {
			lineNo := next.Line + 1
			if complete {
				next.Offset += int64(len(line))
				next.Line = lineNo
			}
			if trimmed := trimEOL(line); len(trimmed) > 0 {
				if err := fn(trimmed, lineNo); err != nil {
					return next, err
				}
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) || errors.Is(readErr, errLineTooLong) {
				// Oversized record: stop and let the next run retry from the
				// same offset rather than emitting a truncated fragment.
				return next, nil
			}
			return next, readErr
		}
	}
}

var errLineTooLong = errors.New("harness-observer: session line exceeds MaxLineBytes")

// readLine returns one line plus whether it was newline-terminated.
func readLine(r *bufio.Reader) (line []byte, complete bool, err error) {
	var buf []byte
	for {
		chunk, readErr := r.ReadSlice('\n')
		switch {
		case readErr == bufio.ErrBufferFull:
			buf = append(buf, chunk...)
			if len(buf) > MaxLineBytes {
				return nil, false, errLineTooLong
			}
			continue
		case readErr != nil:
			// End of file inside an unterminated line.
			buf = append(buf, chunk...)
			return buf, false, io.EOF
		case len(buf) == 0:
			return chunk, true, nil
		default:
			return append(buf, chunk...), true, nil
		}
	}
}

func trimEOL(line []byte) []byte {
	for len(line) > 0 && (line[len(line)-1] == '\n' || line[len(line)-1] == '\r') {
		line = line[:len(line)-1]
	}
	return line
}
