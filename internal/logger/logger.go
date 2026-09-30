package logger

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	// 默认最大日志文件大小 10MB
	DefaultMaxSize = 10 * 1024 * 1024
	// 默认保留的日志文件数量
	DefaultMaxBackups = 3
)

// Logger 日志管理器
type Logger struct {
	mu          sync.Mutex
	file        *os.File
	filePath    string
	maxSize     int64
	maxBackups  int
	currentSize int64
	logger      *log.Logger
	prefix      string
}

// LogManager 全局日志管理
type LogManager struct {
	dataDir       string
	appLogger     *Logger
	singboxLogger *Logger
}

var (
	// 全局日志管理器实例
	manager *LogManager
	once    sync.Once
)

// NewLogger 创建新的日志记录器
func NewLogger(filePath string, prefix string) (*Logger, error) {
	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("创建日志目录失败: %w", err)
	}

	l := &Logger{
		filePath:   filePath,
		maxSize:    DefaultMaxSize,
		maxBackups: DefaultMaxBackups,
		prefix:     prefix,
	}

	if err := l.openFile(); err != nil {
		return nil, err
	}

	return l, nil
}

// openFile 打开或创建日志文件
func (l *Logger) openFile() error {
	file, err := os.OpenFile(l.filePath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return fmt.Errorf("打开日志文件失败: %w", err)
	}

	info, err := file.Stat()
	if err != nil {
		file.Close()
		return fmt.Errorf("获取文件信息失败: %w", err)
	}

	l.file = file
	l.currentSize = info.Size()
	l.logger = log.New(file, l.prefix, log.LstdFlags)

	return nil
}

// rotate 轮转日志文件
func (l *Logger) rotate() error {
	if l.file != nil {
		l.file.Close()
	}

	// 删除最旧的备份
	oldestBackup := fmt.Sprintf("%s.%d", l.filePath, l.maxBackups)
	os.Remove(oldestBackup)

	// 移动现有备份
	for i := l.maxBackups - 1; i >= 1; i-- {
		oldPath := fmt.Sprintf("%s.%d", l.filePath, i)
		newPath := fmt.Sprintf("%s.%d", l.filePath, i+1)
		os.Rename(oldPath, newPath)
	}

	// 移动当前日志到 .1
	os.Rename(l.filePath, l.filePath+".1")

	// 创建新文件
	return l.openFile()
}

// Write 实现 io.Writer 接口
func (l *Logger) Write(p []byte) (n int, err error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	// 检查是否需要轮转
	if l.currentSize+int64(len(p)) > l.maxSize {
		if err := l.rotate(); err != nil {
			return 0, err
		}
	}

	n, err = l.file.Write(p)
	l.currentSize += int64(n)
	return
}

// Printf 格式化日志输出
func (l *Logger) Printf(format string, v ...interface{}) {
	timestamp := time.Now().Format("2006/01/02 15:04:05")
	msg := Redact(fmt.Sprintf(format, v...))
	line := fmt.Sprintf("%s %s%s\n", timestamp, l.prefix, msg)

	// 写入文件
	l.Write([]byte(line))

	// 同时输出到控制台
	fmt.Print(line)
}

// Println 输出一行日志
func (l *Logger) Println(v ...interface{}) {
	timestamp := time.Now().Format("2006/01/02 15:04:05")
	msg := Redact(fmt.Sprint(v...))
	line := fmt.Sprintf("%s %s%s\n", timestamp, l.prefix, msg)

	// 写入文件
	l.Write([]byte(line))

	// 同时输出到控制台
	fmt.Print(line)
}

// WriteRaw 写入原始日志行（不添加时间戳，用于 sing-box 输出）
// 只写入文件，不输出到控制台，避免和程序日志混在一起
func (l *Logger) WriteRaw(line string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	data := Redact(line) + "\n"

	// 检查是否需要轮转
	if l.currentSize+int64(len(data)) > l.maxSize {
		if err := l.rotate(); err != nil {
			fmt.Fprintf(os.Stderr, "日志轮转失败: %v\n", err)
			return
		}
	}

	n, _ := l.file.Write([]byte(data))
	l.currentSize += int64(n)
}

// Close 关闭日志文件
func (l *Logger) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.file != nil {
		return l.file.Close()
	}
	return nil
}

// ReadLastLines 读取最后 n 行日志
func (l *Logger) ReadLastLines(n int) ([]string, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	// 同步文件
	if l.file != nil {
		l.file.Sync()
	}

	// 读取文件
	file, err := os.Open(l.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, fmt.Errorf("打开日志文件失败: %w", err)
	}
	defer file.Close()

	// 使用 ring buffer 来存储最后 n 行
	lines := make([]string, 0, n)
	scanner := bufio.NewScanner(file)

	// 增加 scanner 缓冲区大小以处理长行
	buf := make([]byte, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	for scanner.Scan() {
		lines = append(lines, scanner.Text())
		if len(lines) > n {
			lines = lines[1:]
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("读取日志失败: %w", err)
	}

	return lines, nil
}

// GetFilePath 获取日志文件路径
func (l *Logger) GetFilePath() string {
	return l.filePath
}

// InitLogManager 初始化全局日志管理器
func InitLogManager(dataDir string) error {
	var initErr error
	once.Do(func() {
		logsDir := filepath.Join(dataDir, "logs")

		appLogger, err := NewLogger(filepath.Join(logsDir, "sbm.log"), "[SBM] ")
		if err != nil {
			initErr = fmt.Errorf("初始化应用日志失败: %w", err)
			return
		}

		singboxLogger, err := NewLogger(filepath.Join(logsDir, "singbox.log"), "")
		if err != nil {
			initErr = fmt.Errorf("初始化 sing-box 日志失败: %w", err)
			return
		}

		manager = &LogManager{
			dataDir:       dataDir,
			appLogger:     appLogger,
			singboxLogger: singboxLogger,
		}
	})

	return initErr
}

// GetLogManager 获取全局日志管理器
func GetLogManager() *LogManager {
	return manager
}

// AppLogger 获取应用日志记录器
func (m *LogManager) AppLogger() *Logger {
	return m.appLogger
}

// SingboxLogger 获取 sing-box 日志记录器
func (m *LogManager) SingboxLogger() *Logger {
	return m.singboxLogger
}

// Printf 应用日志快捷方法
func Printf(format string, v ...interface{}) {
	if manager != nil && manager.appLogger != nil {
		manager.appLogger.Printf(format, v...)
	} else {
		log.Print(Redact(fmt.Sprintf(format, v...)))
	}
}

// Println 应用日志快捷方法
func Println(v ...interface{}) {
	if manager != nil && manager.appLogger != nil {
		manager.appLogger.Println(v...)
	} else {
		log.Print(Redact(fmt.Sprint(v...)))
	}
}

// SingboxWriter 返回一个可以用于 sing-box 输出的 Writer
type SingboxWriter struct {
	logger   *Logger
	memLogs  *[]string
	memMu    *sync.RWMutex
	maxLogs  int
	callback func(string) // 可选的回调函数
}

// NewSingboxWriter 创建 sing-box 输出写入器
func NewSingboxWriter(logger *Logger, memLogs *[]string, memMu *sync.RWMutex, maxLogs int) *SingboxWriter {
	return &SingboxWriter{
		logger:  logger,
		memLogs: memLogs,
		memMu:   memMu,
		maxLogs: maxLogs,
	}
}

// Write 实现 io.Writer 接口
func (w *SingboxWriter) Write(p []byte) (n int, err error) {
	return len(p), nil
}

// WriteLine 写入一行日志
func (w *SingboxWriter) WriteLine(line string) {
	// 写入文件
	if w.logger != nil {
		w.logger.WriteRaw(line)
	}

	// 写入内存
	if w.memLogs != nil && w.memMu != nil {
		w.memMu.Lock()
		*w.memLogs = append(*w.memLogs, line)
		if len(*w.memLogs) > w.maxLogs {
			*w.memLogs = (*w.memLogs)[len(*w.memLogs)-w.maxLogs:]
		}
		w.memMu.Unlock()
	}
}

// ReadAppLogs 读取应用日志
func ReadAppLogs(lines int) ([]string, error) {
	if manager == nil || manager.appLogger == nil {
		return []string{}, nil
	}
	return manager.appLogger.ReadLastLines(lines)
}

// ReadSingboxLogs 读取 sing-box 日志
func ReadSingboxLogs(lines int) ([]string, error) {
	if manager == nil || manager.singboxLogger == nil {
		return []string{}, nil
	}
	return manager.singboxLogger.ReadLastLines(lines)
}

// MultiWriter 同时写入多个目标
type MultiWriter struct {
	writers []io.Writer
}

// NewMultiWriter 创建多目标写入器
func NewMultiWriter(writers ...io.Writer) *MultiWriter {
	return &MultiWriter{writers: writers}
}

// Write 写入所有目标
func (mw *MultiWriter) Write(p []byte) (n int, err error) {
	for _, w := range mw.writers {
		n, err = w.Write(p)
		if err != nil {
			return
		}
	}
	return len(p), nil
}
