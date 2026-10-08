// Package dnsquery 将 LAN DNS 查询从内核日志独立归档，不参与 DNS 转发或策略判断。
package dnsquery

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/netip"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/xiaobei/singbox-manager/internal/logger"
)

const (
	RetentionDays = 7
	MaxRecords    = 100000
	MaxBytes      = 64 * 1024 * 1024
)

type Record struct {
	At     int64  `json:"at"`
	Source string `json:"source"`
	Domain string `json:"domain"`
	Type   string `json:"type"`
}
type Domain struct {
	Domain   string `json:"domain"`
	Count    int    `json:"count"`
	LastSeen int64  `json:"last_seen"`
}
type Filter struct {
	Source, Search, Type, Sort string
	Since, Until               int64
	Offset, Limit              int
}
type Result struct {
	Records     []Record `json:"records"`
	Domains     []Domain `json:"domains"`
	Sources     []string `json:"sources"`
	Total       int      `json:"total"`
	DomainTotal int      `json:"domain_total"`
}
type Stats struct {
	Count     int    `json:"count"`
	LastScan  int64  `json:"last_scan"`
	LastQuery int64  `json:"last_query"`
	Gaps      int    `json:"gaps"`
	Error     string `json:"error"`
}
type client struct {
	Address string `json:"address"`
	Seen    int64  `json:"seen"`
}

// 同一帧提交查询、来源关联与读取游标；崩溃重放不会重复导入已提交记录。
type frame struct {
	Cursor  string            `json:"cursor"`
	Sources map[string]client `json:"sources,omitempty"`
	Records []Record          `json:"records,omitempty"`
	Reset   bool              `json:"reset,omitempty"`
	Removed []string          `json:"removed,omitempty"`
	Gaps    int               `json:"gaps"`
}
type storedRecord struct {
	Record
	file string
}
type Journal struct {
	mu               sync.Mutex
	dir, cursor      string
	loaded           bool
	clients          map[string]client
	records          []storedRecord
	segmentRecords   map[string]int
	segmentLastQuery map[string]int64
	stats            Stats
	segmentBytes     int64
	maxSegments      int
}

func New(dir string) *Journal {
	return &Journal{dir: dir, clients: make(map[string]client), segmentBytes: 4 * 1024 * 1024, maxSegments: 16}
}

var logLine = regexp.MustCompile(`^([+-]\d{4} \d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}) \S+ \[(\d+) [^\]]+\] (.*)$`)
var inbound = regexp.MustCompile(`^inbound/[^\[]+\[([^\]]+)\]: inbound (?:packet )?connection from (\S+)$`)
var question = regexp.MustCompile(`^dns: (?:exchange|rejected resolver discovery query) (\S+) IN ([A-Z0-9]+)$`)

func (j *Journal) parse(lines []string, enabled bool, now time.Time) frame {
	f := frame{Sources: make(map[string]client), Gaps: j.stats.Gaps}
	for _, line := range lines {
		if strings.Contains(line, " INFO sing-box started (") {
			j.clients = make(map[string]client)
			f.Sources = make(map[string]client)
			f.Reset = true
		}
		p := logLine.FindStringSubmatch(line)
		if len(p) != 4 {
			continue
		}
		at, err := time.Parse("-0700 2006-01-02 15:04:05", p[1])
		if err != nil || at.After(now.Add(time.Minute)) {
			continue
		}
		if m := inbound.FindStringSubmatch(p[3]); len(m) == 3 {
			delete(j.clients, p[2])
			delete(f.Sources, p[2])
			f.Removed = append(f.Removed, p[2])
			if m[1] != "lan-dns" {
				continue
			}
			addr, err := netip.ParseAddrPort(m[2])
			if err != nil {
				continue
			}
			v := client{Address: addr.Addr().Unmap().String(), Seen: at.UnixMilli()}
			j.clients[p[2]] = v
			f.Sources[p[2]] = v
			continue
		}
		m := question.FindStringSubmatch(p[3])
		if len(m) != 3 {
			continue
		}
		v, ok := j.clients[p[2]]
		if !ok {
			continue
		} // 排除内部解析，缺少来源时不猜测设备。
		v.Seen = at.UnixMilli()
		j.clients[p[2]] = v
		f.Sources[p[2]] = v
		if !enabled || now.Sub(at) > RetentionDays*24*time.Hour {
			continue
		}
		domain := strings.ToLower(strings.TrimSuffix(m[1], "."))
		if domain == "" || len(domain) > 253 {
			continue
		}
		f.Records = append(f.Records, Record{At: at.UnixMilli(), Source: v.Address, Domain: domain, Type: m[2]})
	}
	// UDP 会话每次查询都更新 Seen，长期活跃会话不会因最初建连时间而失去来源。
	for id, v := range j.clients {
		if now.UnixMilli()-v.Seen > int64(30*time.Minute/time.Millisecond) {
			delete(j.clients, id)
		}
	}
	for len(j.clients) > 8192 {
		oldest := ""
		for id, v := range j.clients {
			if oldest == "" || v.Seen < j.clients[oldest].Seen {
				oldest = id
			}
		}
		delete(j.clients, oldest)
	}
	return f
}

func (j *Journal) apply(f frame, path string, now time.Time) {
	j.segmentRecords[path] += len(f.Records)
	if f.Reset {
		j.clients = make(map[string]client)
	}
	for _, id := range f.Removed {
		delete(j.clients, id)
	}
	for id, v := range f.Sources {
		j.clients[id] = v
	}
	for _, r := range f.Records {
		if r.At > j.segmentLastQuery[path] {
			j.segmentLastQuery[path] = r.At
		}
		if r.At >= now.Add(-RetentionDays*24*time.Hour).UnixMilli() {
			j.records = append(j.records, storedRecord{r, path})
		}
	}
	j.cursor = f.Cursor
	j.stats.Gaps = f.Gaps
	for id, v := range j.clients {
		if now.UnixMilli()-v.Seen > int64(30*time.Minute/time.Millisecond) {
			delete(j.clients, id)
		}
	}
	for len(j.clients) > 8192 {
		oldest := ""
		for id, v := range j.clients {
			if oldest == "" || v.Seen < j.clients[oldest].Seen {
				oldest = id
			}
		}
		delete(j.clients, oldest)
	}
	if len(j.records) > MaxRecords {
		j.records = j.records[len(j.records)-MaxRecords:]
	}
}

func (j *Journal) files() ([]string, error) {
	return filepath.Glob(filepath.Join(j.dir, "journal-*.jsonl"))
}

func (j *Journal) load(now time.Time) error {
	if j.loaded {
		return nil
	}
	if err := os.MkdirAll(j.dir, 0700); err != nil {
		return err
	}
	if err := os.Chmod(j.dir, 0700); err != nil {
		return err
	}
	j.clients = make(map[string]client)
	j.records = nil
	j.segmentRecords = make(map[string]int)
	j.segmentLastQuery = make(map[string]int64)
	j.cursor = ""
	j.stats.Gaps = 0
	files, err := j.files()
	if err != nil {
		return err
	}
	for index, path := range files {
		file, err := os.OpenFile(path, os.O_RDWR, 0600)
		if err != nil {
			return err
		}
		err = func() error {
			defer file.Close()
			if err := file.Chmod(0600); err != nil {
				return err
			}
			reader := bufio.NewReaderSize(file, 1024*1024)
			var offset int64
			for {
				line, err := reader.ReadBytes('\n')
				if err == io.EOF {
					if len(line) > 0 {
						if index != len(files)-1 {
							return fmt.Errorf("归档包含未完成的历史帧")
						}
						return file.Truncate(offset)
					}
					return nil
				}
				if err != nil {
					return err
				}
				if len(line) > 1024*1024 {
					return fmt.Errorf("归档帧超过限制")
				}
				var f frame
				if json.Unmarshal(line, &f) != nil || f.Cursor == "" {
					return fmt.Errorf("DNS 归档损坏")
				}
				j.apply(f, path, now)
				offset += int64(len(line))
			}
		}()
		if err != nil {
			return err
		}
	}
	j.loaded = true
	return j.prune(now)
}

func (j *Journal) append(f frame, now time.Time) (string, error) {
	data, err := json.Marshal(f)
	if err != nil {
		return "", err
	}
	data = append(data, '\n')
	files, err := j.files()
	if err != nil {
		return "", err
	}
	path := ""
	if len(files) > 0 {
		path = files[len(files)-1]
		info, err := os.Stat(path)
		if err != nil {
			return "", err
		}
		lastQuery := j.segmentLastQuery[path]
		if info.Size()+int64(len(data)) > j.segmentBytes || lastQuery > 0 && lastQuery < now.Add(-RetentionDays*24*time.Hour).UnixMilli() {
			path = ""
		}
	}
	if path == "" {
		id := now.UnixNano()
		if len(files) > 0 {
			previous, err := strconv.ParseInt(strings.TrimSuffix(strings.TrimPrefix(filepath.Base(files[len(files)-1]), "journal-"), ".jsonl"), 10, 64)
			if err != nil {
				return "", fmt.Errorf("DNS 归档文件名无效")
			}
			if id <= previous {
				id = previous + 1
			}
		}
		for {
			path = filepath.Join(j.dir, fmt.Sprintf("journal-%020d.jsonl", id))
			if _, err := os.Stat(path); os.IsNotExist(err) {
				break
			}
			id++
		}
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return "", err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return "", err
	}
	n, err := file.Write(data)
	if err == nil && n != len(data) {
		err = io.ErrShortWrite
	}
	if err == nil {
		err = file.Sync()
	}
	if err != nil {
		_ = file.Truncate(info.Size())
		return "", err
	}
	return path, nil
}

func (j *Journal) prune(now time.Time) error {
	files, err := j.files()
	if err != nil {
		return err
	}
	// 没有新日志时也回收过期的最后一个数据段，保留只含游标的检查点。
	if len(files) > 0 && j.segmentLastQuery[files[len(files)-1]] > 0 && j.segmentLastQuery[files[len(files)-1]] < now.Add(-RetentionDays*24*time.Hour).UnixMilli() {
		live := cloneClients(j.clients)
		for id, v := range live {
			if now.UnixMilli()-v.Seen > int64(30*time.Minute/time.Millisecond) {
				delete(live, id)
			}
		}
		f := frame{Cursor: j.cursor, Sources: live, Reset: true, Gaps: j.stats.Gaps}
		path, err := j.append(f, now)
		if err != nil {
			return err
		}
		j.apply(f, path, now)
		files = append(files, path)
	}
	total := 0
	for _, count := range j.segmentRecords {
		total += count
	}
	removed := make(map[string]bool)
	for index, path := range files {
		info, err := os.Stat(path)
		if err != nil {
			return err
		}
		if index == len(files)-1 {
			break
		} // 保留最新游标，即使记录已经过期。
		lastQuery := j.segmentLastQuery[path]
		if len(files)-index > j.maxSegments || now.Sub(info.ModTime()) > RetentionDays*24*time.Hour || lastQuery > 0 && lastQuery < now.Add(-RetentionDays*24*time.Hour).UnixMilli() || total > MaxRecords {
			if err := os.Remove(path); err != nil {
				return err
			}
			removed[path] = true
			total -= j.segmentRecords[path]
			delete(j.segmentRecords, path)
			delete(j.segmentLastQuery, path)
		}
	}
	kept := j.records[:0]
	cutoff := now.Add(-RetentionDays * 24 * time.Hour).UnixMilli()
	for _, r := range j.records {
		if !removed[r.file] && r.At >= cutoff {
			kept = append(kept, r)
		}
	}
	j.records = kept
	return nil
}

func cloneClients(v map[string]client) map[string]client {
	r := make(map[string]client, len(v))
	for k, x := range v {
		r[k] = x
	}
	return r
}

// Scan 的读取量有界；后端定时调用，与页面是否打开无关。
func (j *Journal) Scan(path string, enabled bool, now time.Time) (result error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	defer func() {
		j.stats.LastScan = now.UnixMilli()
		j.stats.Error = ""
		if result != nil {
			j.stats.Error = "DNS 归档读取或写入失败，采集已暂停"
		}
	}()
	if err := j.load(now); err != nil {
		return err
	}
	for index := 0; index < 32; index++ {
		batch, err := logger.ReadLogBatch(path, j.cursor, logger.MaxLogBatchLines)
		if err != nil {
			return err
		}
		if batch.Cursor == j.cursor {
			break
		}
		before := cloneClients(j.clients)
		if batch.Gap {
			j.clients = make(map[string]client)
		}
		f := j.parse(batch.Lines, enabled, now)
		f.Cursor = batch.Cursor
		if batch.Gap {
			f.Reset = true
			f.Gaps++
		}
		file, err := j.append(f, now)
		j.clients = before
		if err != nil {
			return err
		}
		j.apply(f, file, now)
		if !batch.More {
			break
		}
	}
	return j.prune(now)
}

func (j *Journal) Status() Stats {
	j.mu.Lock()
	defer j.mu.Unlock()
	s := j.stats
	s.Count = len(j.records)
	for _, r := range j.records {
		if r.At > s.LastQuery {
			s.LastQuery = r.At
		}
	}
	return s
}

func (j *Journal) Search(f Filter, now time.Time) Result {
	j.mu.Lock()
	defer j.mu.Unlock()
	r := Result{Records: []Record{}, Domains: []Domain{}, Sources: []string{}}
	domains := make(map[string]Domain)
	sources := make(map[string]bool)
	search := strings.ToLower(f.Search)
	cutoff := now.Add(-RetentionDays * 24 * time.Hour).UnixMilli()
	for i := len(j.records) - 1; i >= 0; i-- {
		x := j.records[i].Record
		if x.At < cutoff {
			continue
		}
		sources[x.Source] = true
		if f.Source != "" && x.Source != f.Source || f.Type != "" && x.Type != f.Type || f.Since > 0 && x.At < f.Since || f.Until > 0 && x.At > f.Until || !strings.Contains(x.Domain, search) {
			continue
		}
		d := domains[x.Domain]
		d.Domain = x.Domain
		d.Count++
		if x.At > d.LastSeen {
			d.LastSeen = x.At
		}
		domains[x.Domain] = d
		if r.Total >= f.Offset && len(r.Records) < f.Limit {
			r.Records = append(r.Records, x)
		}
		r.Total++
	}
	for _, d := range domains {
		r.Domains = append(r.Domains, d)
	}
	sort.Slice(r.Domains, func(i, k int) bool {
		a, b := r.Domains[i], r.Domains[k]
		if f.Sort == "recent" && a.LastSeen != b.LastSeen {
			return a.LastSeen > b.LastSeen
		}
		if a.Count != b.Count {
			return a.Count > b.Count
		}
		return a.Domain < b.Domain
	})
	r.DomainTotal = len(r.Domains)
	end := f.Offset + f.Limit
	if end > len(r.Domains) {
		end = len(r.Domains)
	}
	if f.Offset >= len(r.Domains) {
		r.Domains = []Domain{}
	} else {
		r.Domains = r.Domains[f.Offset:end]
	}
	for s := range sources {
		r.Sources = append(r.Sources, s)
	}
	sort.Strings(r.Sources)
	return r
}

// CSV 内容按文本导出，防止域名被表格软件解释为公式。
func SafeCSV(value string) string {
	trimmed := strings.TrimLeft(value, " \t\r\n")
	if len(trimmed) > 0 && strings.ContainsRune("=+-@", rune(trimmed[0])) {
		return "'" + value
	}
	return value
}
