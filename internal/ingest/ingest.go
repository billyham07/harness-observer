// Package ingest drives incremental, idempotent collection: discover session
// files, resume each one from its checkpoint, persist normalized turns.
//
// Correctness rules that shape this package:
//   - Never rewrite or move a source session file.
//   - A file whose size and mtime are unchanged is not re-read.
//   - A file that was truncated or replaced is re-read from byte zero.
//   - A partially written trailing line is not parsed until it is complete.
//   - Re-ingesting the same evidence must not double count.
package ingest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/billyham07/harness-observer/internal/source"
	"github.com/billyham07/harness-observer/internal/store"
)

// ParserVersion invalidates checkpoints when normalization changes. Bump it
// whenever an adapter's output for identical bytes would differ; the next run
// then re-reads affected files instead of trusting stale offsets.
const ParserVersion = 6

// headFingerprintBytes is how much of a file's head identifies it. Session
// files start with a session header, so a changed prefix means the file was
// replaced or rotated rather than appended to.
const headFingerprintBytes = 4096

type Stats struct {
	Source          string    `json:"source"`
	Roots           []string  `json:"roots"`
	FilesDiscovered int       `json:"files_discovered"`
	FilesParsed     int       `json:"files_parsed"`
	FilesSkipped    int       `json:"files_skipped"`
	FilesRewound    int       `json:"files_rewound"`
	TurnsParsed     int       `json:"turns_parsed"`
	TurnsIngested   int       `json:"turns_ingested"`
	NewestActivity  time.Time `json:"newest_activity,omitempty"`
	Warnings        []string  `json:"warnings,omitempty"`
}

type Ingestor struct {
	DB      *store.DB
	Sources []source.FileSource
	// Window filters which turns are persisted. Leave zero to ingest everything
	// found; checkpoints then cover the whole file.
	Window source.Window
}

func (in Ingestor) Run(ctx context.Context) ([]Stats, error) {
	var all []Stats
	for _, s := range in.Sources {
		st, err := in.runSource(ctx, s)
		all = append(all, st)
		if err != nil {
			return all, err
		}
	}
	return all, nil
}

func (in Ingestor) runSource(ctx context.Context, s source.FileSource) (Stats, error) {
	st := Stats{Source: s.Name(), Roots: s.Roots()}
	if err := s.Prepare(ctx); err != nil {
		st.Warnings = append(st.Warnings, fmt.Sprintf("prepare failed: %v", err))
		return st, nil
	}
	files, err := s.Files(ctx)
	if err != nil {
		st.Warnings = append(st.Warnings, fmt.Sprintf("discovery failed: %v", err))
	}
	sort.Strings(files)
	st.FilesDiscovered = len(files)

	for _, path := range files {
		if err := ctx.Err(); err != nil {
			return st, err
		}
		info, statErr := os.Stat(path)
		if statErr != nil {
			st.Warnings = append(st.Warnings, fmt.Sprintf("stat %s: %v", path, statErr))
			continue
		}
		fingerprint, fpErr := headFingerprint(path, headPrefixLen(info.Size()))
		if fpErr != nil {
			st.Warnings = append(st.Warnings, fmt.Sprintf("fingerprint %s: %v", path, fpErr))
			continue
		}

		cp, found, cpErr := in.DB.Checkpoint(ctx, s.Name(), path)
		if cpErr != nil {
			return st, cpErr
		}
		// Compare against the checkpoint over the prefix length the checkpoint
		// was taken with, not the current one: an append-only file grows past
		// its old prefix and would otherwise look like a different file.
		sameHead := false
		if found {
			previous, prefixLen := splitFingerprint(cp.HeadFingerprint)
			if info.Size() >= prefixLen {
				current, cmpErr := headFingerprint(path, prefixLen)
				if cmpErr != nil {
					st.Warnings = append(st.Warnings, fmt.Sprintf("fingerprint %s: %v", path, cmpErr))
					continue
				}
				currentHash, _ := splitFingerprint(current)
				sameHead = currentHash == previous
			}
		}
		start := source.FileState{}
		cumulative := int64(0)
		if watcher, watchOK := s.(source.ContentWatch); watchOK {
			token, useWatch, tokErr := watcher.ContentToken(path)
			if tokErr != nil {
				st.Warnings = append(st.Warnings, fmt.Sprintf("watch %s: %v", path, tokErr))
				continue
			}
			if useWatch {
				switch {
				case !found:
				case cp.ParserVersion != ParserVersion:
					st.FilesRewound++
				case watcher.SameContent(cp.State, token):
					st.FilesSkipped++
					continue
				default:
					start = cp.State
					cumulative = cp.TurnsIngested
				}
				goto parse
			}
		}
		switch {
		case !found:
		case cp.ParserVersion != ParserVersion:
			// Parser upgrade: rebuild this file's evidence from the start.
			st.FilesRewound++
		case !sameHead || info.Size() < cp.State.Offset:
			// Truncated, rotated or rewritten in place.
			st.FilesRewound++
		case info.Size() == cp.Size && info.ModTime().UnixMilli() == cp.MTimeUnixMs:
			st.FilesSkipped++
			continue
		default:
			start = cp.State
			cumulative = cp.TurnsIngested
		}
	parse:

		turns, next, parseErr := s.ParseFile(path, start, in.Window)
		if parseErr != nil {
			// One unreadable file must not abort unrelated sessions; the
			// checkpoint stays where it was so the next run retries it.
			st.Warnings = append(st.Warnings, fmt.Sprintf("parse %s: %v", path, parseErr))
			continue
		}
		st.FilesParsed++
		st.TurnsParsed += len(turns)

		// Turns and their checkpoint commit together. If the process dies
		// mid-file the whole unit is rolled back and the next run re-reads that
		// file's tail; a checkpoint can never outlive the rows it accounts for.
		checkpoint := store.Checkpoint{
			Source:          s.Name(),
			Path:            path,
			Size:            info.Size(),
			MTimeUnixMs:     info.ModTime().UnixMilli(),
			HeadFingerprint: fingerprint,
			State:           next,
			ParserVersion:   ParserVersion,
			TurnsIngested:   cumulative + int64(len(turns)),
		}
		inserted, insErr := in.DB.CommitFile(ctx, turns, &checkpoint)
		if insErr != nil {
			return st, insErr
		}
		st.TurnsIngested += inserted
		for _, t := range turns {
			if t.Timestamp.After(st.NewestActivity) {
				st.NewestActivity = t.Timestamp
			}
		}
	}
	return st, nil
}

// headPrefixLen is how many leading bytes identify a file, capped so a huge
// transcript is not re-hashed on every scan.
func headPrefixLen(size int64) int64 {
	if size < headFingerprintBytes {
		return size
	}
	return headFingerprintBytes
}

// headFingerprint hashes exactly n leading bytes and encodes the length with
// the digest ("<n>:<hex>") so a later comparison can reproduce it.
func headFingerprint(path string, n int64) (string, error) {
	if n <= 0 {
		return "0:", nil
	}
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	buf := make([]byte, n)
	read, err := io.ReadFull(f, buf)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return "", err
	}
	sum := sha256.Sum256(buf[:read])
	return fmt.Sprintf("%d:%s", read, hex.EncodeToString(sum[:])), nil
}

func splitFingerprint(v string) (hash string, prefixLen int64) {
	i := strings.IndexByte(v, ':')
	if i < 0 {
		return v, headFingerprintBytes
	}
	n, err := strconv.ParseInt(v[:i], 10, 64)
	if err != nil {
		return v[i+1:], headFingerprintBytes
	}
	return v[i+1:], n
}
