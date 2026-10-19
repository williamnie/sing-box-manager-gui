package logger

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadTailReturnsLastLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.log")
	var b strings.Builder
	// 超过初始 64 KiB 窗口，覆盖窗口扩张和首行截断。
	for i := 0; i < 5000; i++ {
		fmt.Fprintf(&b, "line-%04d %s\r\n", i, strings.Repeat("x", 40))
	}
	if err := os.WriteFile(path, []byte(b.String()), 0600); err != nil {
		t.Fatal(err)
	}
	lines, err := ReadTail(path, 1500)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 1500 || !strings.HasPrefix(lines[0], "line-3500 ") || !strings.HasPrefix(lines[1499], "line-4999 ") || strings.HasSuffix(lines[0], "\r") {
		t.Fatalf("unexpected tail: %d %q %q", len(lines), lines[0], lines[len(lines)-1])
	}
	if lines, _ = ReadTail(path, 1<<30); len(lines) != MaxReadLines {
		t.Fatalf("limit not applied: %d", len(lines))
	}
	if err := os.WriteFile(path, []byte("a\nb"), 0600); err != nil {
		t.Fatal(err)
	}
	if lines, _ = ReadTail(path, 10); strings.Join(lines, ",") != "a,b" {
		t.Fatalf("short file: %q", lines)
	}
	if lines, _ = ReadTail(filepath.Join(t.TempDir(), "missing"), 10); len(lines) != 0 {
		t.Fatal("missing file should be empty")
	}
}
