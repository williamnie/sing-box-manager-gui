package logger

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func appendLog(t *testing.T, path, value string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err = f.WriteString(value); err != nil {
		t.Fatal(err)
	}
}
func readBatch(t *testing.T, path, cursor string, limit int) LogBatch {
	t.Helper()
	batch, err := ReadLogBatch(path, cursor, limit)
	if err != nil {
		t.Fatal(err)
	}
	return batch
}

func TestCursorTailIncrementalPartialAndRedaction(t *testing.T) {
	path := filepath.Join(t.TempDir(), "singbox.log")
	appendLog(t, path, "old\ninfo first\ninfo second\n")
	initial := readBatch(t, path, "", 2)
	if strings.Join(initial.Lines, "|") != "info first|info second" {
		t.Fatalf("wrong initial tail: %#v", initial)
	}
	appendLog(t, path, "INFO token=private-value\npartial")
	next := readBatch(t, path, initial.Cursor, 10)
	if len(next.Lines) != 1 || strings.Contains(next.Lines[0], "private-value") {
		t.Fatalf("redaction or partial: %#v", next)
	}
	again := readBatch(t, path, next.Cursor, 10)
	if len(again.Lines) != 0 {
		t.Fatal("partial was emitted")
	}
	appendLog(t, path, " complete\n")
	completed := readBatch(t, path, again.Cursor, 10)
	if strings.Join(completed.Lines, "") != "partial complete" {
		t.Fatalf("partial lost: %#v", completed)
	}
}
func TestCursorReconnectAcrossThreeRotationsAndExpiredCursor(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sbm.log")
	appendLog(t, path, "start\n")
	first := readBatch(t, path, "", 10)
	for i := 0; i < 3; i++ {
		appendLog(t, path, "before rotation\n")
		for n := 2; n >= 1; n-- {
			_ = os.Rename(path+"."+string(rune('0'+n)), path+"."+string(rune('0'+n+1)))
		}
		if err := os.Rename(path, path+".1"); err != nil {
			t.Fatal(err)
		}
		appendLog(t, path, "after rotation\n")
	}
	next := readBatch(t, path, first.Cursor, 20)
	if next.Gap || len(next.Lines) != 6 {
		t.Fatalf("rotation lost lines: %#v", next)
	}
	if err := os.Remove(path + ".3"); err != nil {
		t.Fatal(err)
	}
	expired := readBatch(t, path, first.Cursor, 20)
	if !expired.Gap {
		t.Fatal("expired cursor must report a gap")
	}
}
func TestCursorRejectsMalformedAndDetectsTruncation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "singbox.log")
	appendLog(t, path, "original text\n")
	first := readBatch(t, path, "", 10)
	if _, err := ReadLogBatch(path, "not-a-cursor", 10); err == nil {
		t.Fatal("invalid cursor accepted")
	}
	if _, err := ReadLogBatch(path, strings.Repeat("a", 2048), 10); err == nil {
		t.Fatal("oversized cursor accepted")
	}
	if err := os.WriteFile(path, []byte("replacement text which is longer\n"), 0600); err != nil {
		t.Fatal(err)
	}
	next := readBatch(t, path, first.Cursor, 10)
	if !next.Gap || len(next.Lines) != 1 {
		t.Fatalf("rewrite not detected: %#v", next)
	}
}
func TestCursorBoundsLongLinesAndDoesNotExposeFragments(t *testing.T) {
	path := filepath.Join(t.TempDir(), "singbox.log")
	initial := readBatch(t, path, "", 10)
	appendLog(t, path, "password="+strings.Repeat("s", MaxLogLineBytes+100)+"\nnormal\n")
	batch := readBatch(t, path, initial.Cursor, 10)
	if len(batch.Lines) != 2 || strings.Contains(batch.Lines[0], "ssss") || batch.Lines[1] != "normal" {
		t.Fatalf("unsafe long line: %#v", batch)
	}
}
func TestCursorCreatedAfterEmptyLogAndNoDuplicateReads(t *testing.T) {
	path := filepath.Join(t.TempDir(), "singbox.log")
	first := readBatch(t, path, "", 10)
	appendLog(t, path, "created\n")
	next := readBatch(t, path, first.Cursor, 10)
	if next.Gap || strings.Join(next.Lines, "") != "created" {
		t.Fatalf("empty-to-created: %#v", next)
	}
	if len(readBatch(t, path, next.Cursor, 10).Lines) != 0 {
		t.Fatal("replayed log")
	}
}

func TestCursorPartialLongLineAcrossBatchesAndRotatedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "singbox.log")
	first := readBatch(t, path, "", 1)
	appendLog(t, path, strings.Repeat("s", MaxLogBatchBytes+20))
	batch := readBatch(t, path, first.Cursor, 1)
	if len(batch.Lines) != 0 || !batch.More {
		t.Fatalf("partial long line emitted: %#v", batch)
	}
	batch = readBatch(t, path, batch.Cursor, 1)
	if len(batch.Lines) != 0 {
		t.Fatal("partial fragment exposed")
	}
	if err := os.Rename(path, path+".1"); err != nil {
		t.Fatal(err)
	}
	appendLog(t, path, "new file\n")
	batch = readBatch(t, path, batch.Cursor, 1)
	if len(batch.Lines) != 1 || !batch.More || strings.Contains(batch.Lines[0], "sss") {
		t.Fatalf("rotated long line bounds: %#v", batch)
	}
	batch = readBatch(t, path, batch.Cursor, 1)
	if strings.Join(batch.Lines, "") != "new file" {
		t.Fatalf("new file lost: %#v", batch)
	}
}

func TestCursorBatchesDoNotLoseRecordsAtByteBoundary(t *testing.T) {
	path := filepath.Join(t.TempDir(), "singbox.log")
	first := readBatch(t, path, "", 1)
	line := strings.Repeat("x", 1000) + "\n"
	appendLog(t, path, strings.Repeat(line, 600))
	total := 0
	cursor := first.Cursor
	for i := 0; i < 10; i++ {
		batch := readBatch(t, path, cursor, 1000)
		total += len(batch.Lines)
		if batch.Cursor == cursor {
			break
		}
		cursor = batch.Cursor
	}
	if total != 600 {
		t.Fatalf("batch boundary lost/duplicated lines: %d", total)
	}
}

func TestCursorIsBoundToSourceFile(t *testing.T) {
	dir := t.TempDir()
	first := readBatch(t, filepath.Join(dir, "sbm.log"), "", 10)
	if _, err := ReadLogBatch(filepath.Join(dir, "singbox.log"), first.Cursor, 10); err != ErrInvalidLogCursor {
		t.Fatalf("cross-source cursor accepted: %v", err)
	}
}

func TestCursorInitialTailAcrossFilesAndUnterminatedBackup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "singbox.log")
	appendLog(t, path+".1", "old without newline")
	appendLog(t, path, "a\nb\n")
	batch := readBatch(t, path, "", 2)
	if strings.Join(batch.Lines, "|") != "a|b" {
		t.Fatalf("initial tail crossed wrong boundary: %#v", batch)
	}
	batch = readBatch(t, path, "", 3)
	if strings.Join(batch.Lines, "|") != "old without newline|a|b" {
		t.Fatalf("missing unterminated backup: %#v", batch)
	}
}
