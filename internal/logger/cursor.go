package logger

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"syscall"
)

const (
	MaxLogLineBytes  = 64 * 1024
	MaxLogBatchBytes = 256 * 1024
	MaxLogBatchLines = 1000
)

var ErrInvalidLogCursor = errors.New("无效的日志游标")

// LogBatch 的游标位于最后一条完整记录之后，客户端可安全用于断线续接。
// Gap 表示游标对应的历史已经超出轮转保留范围或文件被覆盖。
type LogBatch struct {
	Lines  []string `json:"lines"`
	Cursor string   `json:"cursor"`
	Gap    bool     `json:"gap"`
	More   bool     `json:"more"`
}
type logCursor struct {
	Version  int    `json:"v"`
	Path     string `json:"p"`
	File     string `json:"f"`
	Offset   int64  `json:"o"`
	Anchor   string `json:"a,omitempty"`
	Skipping bool   `json:"s,omitempty"`
}
type logFile struct {
	file *os.File
	id   string
	size int64
}

func pathDigest(path string) string {
	sum := sha256.Sum256([]byte(path))
	return hex.EncodeToString(sum[:8])
}
func encodeLogCursor(cursor logCursor) string {
	data, _ := json.Marshal(cursor)
	return base64.RawURLEncoding.EncodeToString(data)
}
func decodeLogCursor(path, token string) (logCursor, error) {
	var cursor logCursor
	if len(token) > 512 {
		return cursor, ErrInvalidLogCursor
	}
	data, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || json.Unmarshal(data, &cursor) != nil || cursor.Version != 1 || cursor.Path != pathDigest(path) || cursor.Offset < 0 || len(cursor.File) > 64 || (len(cursor.Anchor) != 0 && len(cursor.Anchor) != 64) {
		return cursor, ErrInvalidLogCursor
	}
	return cursor, nil
}
func openLogFiles(path string) ([]logFile, error) {
	files := make([]logFile, 0, DefaultMaxBackups+1)
	seen := map[string]bool{}
	for backup := DefaultMaxBackups; backup >= 0; backup-- {
		name := path
		if backup > 0 {
			name = fmt.Sprintf("%s.%d", path, backup)
		}
		file, err := os.Open(name)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			for _, f := range files {
				f.file.Close()
			}
			return nil, err
		}
		info, err := file.Stat()
		if err != nil {
			file.Close()
			for _, f := range files {
				f.file.Close()
			}
			return nil, err
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || !info.Mode().IsRegular() {
			file.Close()
			continue
		}
		id := fmt.Sprintf("%x-%x", stat.Dev, stat.Ino)
		if seen[id] {
			file.Close()
			continue
		}
		seen[id] = true
		files = append(files, logFile{file: file, id: id, size: info.Size()})
	}
	return files, nil
}
func cursorAnchor(file *os.File, offset int64) string {
	if offset == 0 {
		return ""
	}
	start := offset - 64
	if start < 0 {
		start = 0
	}
	data := make([]byte, int(offset-start))
	n, err := file.ReadAt(data, start)
	if err != nil || n != len(data) {
		return "0000000000000000000000000000000000000000000000000000000000000000"
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// initialLogPosition 只逆向读取有界尾部，避免每次连接扫描整个日志。
func initialLogPosition(files []logFile, limit int) (int, int64, bool) {
	budget := MaxLogBatchBytes
	remaining := limit
	for i := len(files) - 1; i >= 0; i-- {
		f := files[i]
		size := f.size
		if size > int64(budget) {
			size = int64(budget)
		}
		start := f.size - size
		data := make([]byte, int(size))
		n, _ := f.file.ReadAt(data, start)
		data = data[:n]
		budget -= n
		if i < len(files)-1 && len(data) > 0 && data[len(data)-1] != '\n' {
			remaining--
		}
		for j := len(data) - 1; j >= 0; j-- {
			if data[j] == '\n' {
				if remaining == 0 {
					return i, start + int64(j+1), false
				}
				remaining--
			}
		}
		if remaining == 0 || start > 0 || budget == 0 {
			return i, start, start > 0
		}
	}
	return 0, 0, false
}

// ReadLogBatch 读取当前文件及三份轮转备份。每轮只 stat/open 小量文件，并从游标
// 位置读取最多 256 KiB；独立日志中继和管理进程重启不影响接续。
func ReadLogBatch(path, token string, limit int) (LogBatch, error) {
	batch := LogBatch{Lines: []string{}}
	if limit < 1 {
		limit = 500
	}
	if limit > MaxLogBatchLines {
		limit = MaxLogBatchLines
	}
	cursor := logCursor{Version: 1, Path: pathDigest(path)}
	if token != "" {
		var err error
		cursor, err = decodeLogCursor(path, token)
		if err != nil {
			return batch, err
		}
	}
	files, err := openLogFiles(path)
	if err != nil {
		return batch, err
	}
	defer func() {
		for _, f := range files {
			f.file.Close()
		}
	}()
	if len(files) == 0 {
		if cursor.File != "" {
			batch.Gap = true
		}
		cursor.File = ""
		cursor.Offset = 0
		cursor.Anchor = ""
		cursor.Skipping = false
		batch.Cursor = encodeLogCursor(cursor)
		return batch, nil
	}
	index := 0
	fresh := token == ""
	if !fresh && cursor.File != "" {
		index = -1
		for i, f := range files {
			if f.id == cursor.File {
				index = i
				break
			}
		}
		if index < 0 || cursor.Offset > files[index].size || cursor.Anchor != cursorAnchor(files[index].file, cursor.Offset) {
			batch.Gap = true
			fresh = true
		}
	}
	if fresh {
		index, cursor.Offset, cursor.Skipping = initialLogPosition(files, limit)
	}
	budget := MaxLogBatchBytes
	for index < len(files) {
		f := files[index]
		cursor.File = f.id
		available := f.size - cursor.Offset
		if available < 0 {
			batch.Gap = true
			cursor.Offset = 0
			cursor.Skipping = false
			available = f.size
		}
		size := available
		if size > int64(budget) {
			size = int64(budget)
		}
		data := make([]byte, int(size))
		n, readErr := f.file.ReadAt(data, cursor.Offset)
		if readErr != nil && readErr != io.EOF {
			return batch, readErr
		}
		data = data[:n]
		budget -= n
		consumed := 0
		for consumed < len(data) {
			newline := bytes.IndexByte(data[consumed:], '\n')
			if newline < 0 {
				if cursor.Skipping || len(data)-consumed > MaxLogLineBytes {
					cursor.Skipping = true
					consumed = len(data)
				} else if index < len(files)-1 && int64(n) == available {
					batch.Lines = append(batch.Lines, Redact(string(data[consumed:])))
					consumed = len(data)
				}
				break
			}
			line := data[consumed : consumed+newline]
			consumed += newline + 1
			if cursor.Skipping || len(line) > MaxLogLineBytes {
				batch.Lines = append(batch.Lines, "[超长日志行已省略]")
				cursor.Skipping = false
			} else {
				batch.Lines = append(batch.Lines, Redact(string(bytes.TrimSuffix(line, []byte{'\r'}))))
			}
			if len(batch.Lines) >= limit {
				break
			}
		}
		cursor.Offset += int64(consumed)
		cursor.Anchor = cursorAnchor(f.file, cursor.Offset)
		if len(batch.Lines) >= limit || budget <= 0 {
			batch.More = cursor.Offset < f.size || index < len(files)-1
			break
		}
		if cursor.Offset < f.size {
			break
		} // 当前文件尚未结束的一行留待下次写入。
		if index == len(files)-1 {
			break
		}
		if cursor.Skipping {
			batch.Lines = append(batch.Lines, "[超长日志行已省略]")
			cursor.Skipping = false
			if len(batch.Lines) >= limit {
				batch.More = true
				break
			}
		}
		index++
		cursor.Offset = 0
		cursor.Anchor = ""
		cursor.Skipping = false
	}
	batch.Cursor = encodeLogCursor(cursor)
	return batch, nil
}
