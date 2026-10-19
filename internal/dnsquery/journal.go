// Package dnsquery 将 LAN DNS 查询从内核日志独立归档，不参与 DNS 转发或策略判断。
package dnsquery

import (
	"bufio"
	"container/heap"
	"encoding/json"
	"fmt"
	"io"
	"math"
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
	mu               sync.RWMutex
	dir, cursor      string
	loaded           bool
	clients          map[string]client
	records          []storedRecord
	segmentRecords   map[string]int
	segmentLastQuery map[string]int64
	stats            Stats
	segmentBytes     int64
	maxSegments      int
	// pending 是已在内存生效、尚未落盘的无查询检查点；合并写入以减少闪存 fsync。
	pending   *frame
	flushedAt time.Time
	prunedAt  time.Time
	enabled   bool
	// generation 在可见记录变化时递增；查询页每 5 秒轮询，数据未变时直接复用结果。
	generation uint64
	cacheMu    sync.Mutex
	cache      map[Filter]cachedSearch
	cacheGen   uint64
}

type cachedSearch struct {
	result Result
	cutoff int64
	// 缓存有效期到最早的可见记录过期为止。
	validUntil int64
}

const maxCachedSearches = 8

const (
	// 只含游标和来源变化的帧最多延迟这么久落盘；崩溃后重读这段日志只会恢复来源，不会重复导入查询。
	checkpointInterval = time.Minute
	pruneInterval      = time.Minute
	maxPendingRemoved  = 4096
)

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
		// 先用子串排除绝大多数无关日志，避免对每行执行正则。
		if !strings.Contains(line, "] inbound/") && !strings.Contains(line, "] dns: ") {
			continue
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
			// 只记录确实存在的来源删除；其他入站连接与重放状态无关，不写入归档。
			if _, known := j.clients[p[2]]; known {
				f.Removed = append(f.Removed, p[2])
			}
			delete(j.clients, p[2])
			delete(f.Sources, p[2])
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
	if len(f.Records) > 0 {
		j.generation++
	}
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
	j.generation++
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
	if len(kept) != len(j.records) {
		j.generation++
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
	// 重新开启查询记录前先提交关闭期间的游标，崩溃重放不会导入关闭期间的查询。
	if enabled && !j.enabled {
		if err := j.flush(now); err != nil {
			return err
		}
	}
	j.enabled = enabled
	wrote := false
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
		j.clients = before
		merged := mergeFrame(j.pending, f)
		if len(merged.Records) == 0 && !merged.Reset && len(merged.Removed) < maxPendingRemoved && now.Sub(j.flushedAt) < checkpointInterval && !now.Before(j.flushedAt) {
			j.apply(f, "", now)
			delete(j.segmentRecords, "")
			delete(j.segmentLastQuery, "")
			j.pending = &merged
		} else {
			file, err := j.append(merged, now)
			if err != nil {
				return err
			}
			j.apply(merged, file, now)
			j.pending, j.flushedAt, wrote = nil, now, true
		}
		if !batch.More {
			break
		}
	}
	if !wrote && now.Sub(j.prunedAt) < pruneInterval && !now.Before(j.prunedAt) {
		return nil
	}
	j.prunedAt = now
	return j.prune(now)
}

// mergeFrame 把新帧叠加到待提交帧；按 apply 的“先删除后写入”顺序保持结果一致。
func mergeFrame(pending *frame, f frame) frame {
	if pending == nil || f.Reset {
		return f
	}
	merged := *pending
	merged.Sources = cloneClients(pending.Sources)
	merged.Removed = append([]string{}, pending.Removed...)
	merged.Records = append(append([]Record{}, pending.Records...), f.Records...)
	for _, id := range f.Removed {
		delete(merged.Sources, id)
		merged.Removed = append(merged.Removed, id)
	}
	for id, v := range f.Sources {
		merged.Sources[id] = v
	}
	merged.Cursor, merged.Gaps = f.Cursor, f.Gaps
	return merged
}

func (j *Journal) flush(now time.Time) error {
	if j.pending == nil {
		return nil
	}
	file, err := j.append(*j.pending, now)
	if err != nil {
		return err
	}
	j.apply(*j.pending, file, now)
	j.pending, j.flushedAt = nil, now
	return nil
}

// Flush 提交延迟的检查点，供服务停止时调用。
func (j *Journal) Flush() error {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.flush(time.Now())
}

func (j *Journal) Status() Stats {
	j.mu.RLock()
	defer j.mu.RUnlock()
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
	j.mu.RLock()
	defer j.mu.RUnlock()
	cutoff := now.Add(-RetentionDays * 24 * time.Hour).UnixMilli()
	if r, ok := j.cachedSearch(f, cutoff); ok {
		return r
	}
	r := Result{Records: []Record{}, Domains: []Domain{}, Sources: []string{}}
	domains := make(map[string]Domain)
	sources := make(map[string]bool)
	search := strings.ToLower(f.Search)
	validUntil := int64(math.MaxInt64)
	for i := len(j.records) - 1; i >= 0; i-- {
		x := j.records[i].Record
		if x.At < cutoff {
			continue
		}
		if x.At < validUntil {
			validUntil = x.At
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
	r.DomainTotal = len(domains)
	r.Domains = topDomains(domains, f)
	for s := range sources {
		r.Sources = append(r.Sources, s)
	}
	sort.Strings(r.Sources)
	j.storeSearch(f, cachedSearch{result: r, cutoff: cutoff, validUntil: validUntil})
	return copyResult(r)
}

func domainBefore(a, b Domain, sortBy string) bool {
	if sortBy == "recent" && a.LastSeen != b.LastSeen {
		return a.LastSeen > b.LastSeen
	}
	if a.Count != b.Count {
		return a.Count > b.Count
	}
	return a.Domain < b.Domain
}

// domainHeap 以排名最靠后的域名为堆顶，只保留分页所需的前 k 个。
type domainHeap struct {
	items  []Domain
	sortBy string
}

func (h *domainHeap) Len() int           { return len(h.items) }
func (h *domainHeap) Less(i, k int) bool { return domainBefore(h.items[k], h.items[i], h.sortBy) }
func (h *domainHeap) Swap(i, k int)      { h.items[i], h.items[k] = h.items[k], h.items[i] }
func (h *domainHeap) Push(x any)         { h.items = append(h.items, x.(Domain)) }
func (h *domainHeap) Pop() any {
	last := h.items[len(h.items)-1]
	h.items = h.items[:len(h.items)-1]
	return last
}

// topDomains 返回排序后的当前页；域名很多时用有界堆代替全量排序。
func topDomains(domains map[string]Domain, f Filter) []Domain {
	k := f.Offset + f.Limit
	if f.Offset >= len(domains) || f.Limit <= 0 {
		return []Domain{}
	}
	if k > len(domains) {
		k = len(domains)
	}
	h := &domainHeap{items: make([]Domain, 0, k), sortBy: f.Sort}
	for _, d := range domains {
		if h.Len() < k {
			heap.Push(h, d)
		} else if domainBefore(d, h.items[0], f.Sort) {
			h.items[0] = d
			heap.Fix(h, 0)
		}
	}
	sort.Slice(h.items, func(i, k int) bool { return domainBefore(h.items[i], h.items[k], f.Sort) })
	return h.items[f.Offset:]
}

func copyResult(r Result) Result {
	r.Records = append([]Record{}, r.Records...)
	r.Domains = append([]Domain{}, r.Domains...)
	r.Sources = append([]string{}, r.Sources...)
	return r
}

// cachedSearch 只在数据未变化且没有可见记录越过保留期时命中；时钟回拨也视为失效。
func (j *Journal) cachedSearch(f Filter, cutoff int64) (Result, bool) {
	j.cacheMu.Lock()
	defer j.cacheMu.Unlock()
	if j.cacheGen != j.generation {
		return Result{}, false
	}
	entry, ok := j.cache[f]
	if !ok || cutoff < entry.cutoff || cutoff > entry.validUntil {
		return Result{}, false
	}
	return copyResult(entry.result), true
}

func (j *Journal) storeSearch(f Filter, entry cachedSearch) {
	j.cacheMu.Lock()
	defer j.cacheMu.Unlock()
	if j.cacheGen != j.generation || j.cache == nil || len(j.cache) >= maxCachedSearches {
		j.cache = make(map[Filter]cachedSearch)
		j.cacheGen = j.generation
	}
	j.cache[f] = entry
}

// CSV 内容按文本导出，防止域名被表格软件解释为公式。
func SafeCSV(value string) string {
	trimmed := strings.TrimLeft(value, " \t\r\n")
	if len(trimmed) > 0 && strings.ContainsRune("=+-@", rune(trimmed[0])) {
		return "'" + value
	}
	return value
}
