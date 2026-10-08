package dnsquery

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeLog(t *testing.T, path, value string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString(value); err != nil {
		t.Fatal(err)
	}
}

func source(at time.Time, id, address string) string {
	return fmt.Sprintf("%s INFO [%s 0ms] inbound/direct[lan-dns]: inbound packet connection from %s:1234\n", at.Format("-0700 2006-01-02 15:04:05"), id, address)
}
func query(at time.Time, id, domain, kind string) string {
	return fmt.Sprintf("%s DEBUG [%s 0ms] dns: exchange %s IN %s\n", at.Format("-0700 2006-01-02 15:04:05"), id, domain, kind)
}

func TestPersistentQueriesAreNotConnectionSamples(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	dir := t.TempDir()
	path := filepath.Join(dir, "singbox.log")
	j := New(filepath.Join(dir, "dns"))
	writeLog(t, path, source(now, "1", "192.0.2.10")+source(now, "2", "192.0.2.20")+query(now, "1", "Ads.Example.", "A")+query(now, "2", "ads.example.", "AAAA")+query(now, "1", "ads.example.", "A")+query(now, "missing", "internal.test.", "A"))
	if err := j.Scan(path, true, now); err != nil {
		t.Fatal(err)
	}
	r := j.Search(Filter{Source: "192.0.2.10", Search: "ads", Limit: 50}, now)
	if r.Total != 2 || len(r.Domains) != 1 || r.Domains[0].Count != 2 || r.Records[0].Domain != "ads.example" {
		t.Fatalf("bad filtered records: %+v", r)
	}
	// 重启后继续相同 UDP 会话，来源关联和游标须同时恢复，且不能重复导入旧日志。
	j = New(filepath.Join(dir, "dns"))
	if err := j.Scan(path, true, now); err != nil {
		t.Fatal(err)
	}
	writeLog(t, path, query(now, "1", "new.example.", "HTTPS"))
	if err := j.Scan(path, true, now); err != nil {
		t.Fatal(err)
	}
	r = j.Search(Filter{Limit: 50}, now)
	if r.Total != 4 || len(r.Sources) != 2 {
		t.Fatalf("restart lost/duplicated queries: %+v", r)
	}
	if st, err := os.Stat(j.dir); err != nil || st.Mode().Perm() != 0700 {
		t.Fatal("journal directory must be private", err)
	}
}

func TestDisabledQueriesAndInternalResolutionAreExcluded(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	dir := t.TempDir()
	path := filepath.Join(dir, "singbox.log")
	j := New(filepath.Join(dir, "dns"))
	writeLog(t, path, source(now, "1", "192.0.2.10")+query(now, "1", "off.example.", "A"))
	if err := j.Scan(path, false, now); err != nil {
		t.Fatal(err)
	}
	writeLog(t, path, query(now, "1", "on.example.", "A"))
	if err := j.Scan(path, true, now); err != nil {
		t.Fatal(err)
	}
	writeLog(t, path, strings.ReplaceAll(source(now, "2", "192.0.2.20"), "[lan-dns]", "[mixed-in]")+query(now, "2", "internal.example.", "A"))
	if err := j.Scan(path, true, now); err != nil {
		t.Fatal(err)
	}
	r := j.Search(Filter{Limit: 50}, now)
	if r.Total != 1 || r.Records[0].Domain != "on.example" {
		t.Fatalf("unexpected queries: %+v", r)
	}
}

func TestRotationRetentionAndPartialJournalRecovery(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	dir := t.TempDir()
	path := filepath.Join(dir, "singbox.log")
	j := New(filepath.Join(dir, "dns"))
	j.segmentBytes = 512
	j.maxSegments = 3
	writeLog(t, path, source(now, "1", "192.0.2.10"))
	for i := 0; i < 12; i++ {
		writeLog(t, path, query(now, "1", fmt.Sprintf("ads%d.example.", i), "A"))
		if err := j.Scan(path, true, now); err != nil {
			t.Fatal(err)
		}
	}
	files, _ := filepath.Glob(filepath.Join(j.dir, "*.jsonl"))
	if len(files) > 3 {
		t.Fatal("unbounded disk segments", len(files))
	}
	last := files[len(files)-1]
	writeLog(t, last, `{"cursor":`)
	j2 := New(j.dir)
	j2.segmentBytes = 512
	j2.maxSegments = 3
	if err := j2.Scan(path, true, now); err != nil {
		t.Fatal(err)
	}
	if j2.Search(Filter{Limit: 50}, now).Total != j.Search(Filter{Limit: 50}, now).Total {
		t.Fatal("partial trailing frame lost committed data")
	}
	if j2.Search(Filter{Limit: 50}, now.Add(8*24*time.Hour)).Total != 0 {
		t.Fatal("expired query still visible")
	}
	// 内核重启后不得将复用的上下文 ID 归到旧设备。
	writeLog(t, path, now.Format("-0700 2006-01-02 15:04:05")+" INFO sing-box started (0.1s)\n"+query(now, "1", "stale.example.", "A"))
	if err := j2.Scan(path, true, now); err != nil {
		t.Fatal(err)
	}
	if j2.Search(Filter{Search: "stale", Limit: 50}, now).Total != 0 {
		t.Fatal("reused context leaked attribution")
	}
}

func TestMissingLogsAndDiskFailuresRemainVisible(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	dir := t.TempDir()
	path := filepath.Join(dir, "singbox.log")
	j := New(filepath.Join(dir, "dns"))
	writeLog(t, path, source(now, "1", "192.0.2.10")+query(now, "1", "ads.example.", "A"))
	if err := j.Scan(path, true, now); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(source(now, "2", "192.0.2.20")+query(now, "2", "new.example.", "A")), 0600); err != nil {
		t.Fatal(err)
	}
	if err := j.Scan(path, true, now); err != nil {
		t.Fatal(err)
	}
	if j.Status().Gaps != 1 || j.Search(Filter{Limit: 50}, now).Total != 2 {
		t.Fatal("overwritten logs were silently ignored")
	}
	blocked := filepath.Join(dir, "not-a-directory")
	if err := os.WriteFile(blocked, []byte("block"), 0600); err != nil {
		t.Fatal(err)
	}
	bad := New(filepath.Join(blocked, "dns"))
	if bad.Scan(path, true, now) == nil || bad.Status().Error == "" {
		t.Fatal("disk failure presented as successful empty history")
	}
}

func TestExpiryCleanupAndClockRollbackPreserveCursor(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	dir := t.TempDir()
	path := filepath.Join(dir, "singbox.log")
	j := New(filepath.Join(dir, "dns"))
	j.segmentBytes = 200
	writeLog(t, path, source(now, "1", "192.0.2.10")+query(now, "1", "first.example.", "A"))
	if err := j.Scan(path, true, now); err != nil {
		t.Fatal(err)
	}
	writeLog(t, path, query(now, "1", "second.example.", "A"))
	if err := j.Scan(path, true, now.Add(-time.Second)); err != nil {
		t.Fatal(err)
	}
	reopened := New(j.dir)
	if err := reopened.Scan(path, true, now); err != nil {
		t.Fatal(err)
	}
	if reopened.Search(Filter{Limit: 50}, now).Total != 2 {
		t.Fatal("clock rollback duplicated committed records")
	}
	if err := reopened.Scan(path, true, now.Add(8*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	files, _ := filepath.Glob(filepath.Join(j.dir, "*.jsonl"))
	for _, path := range files {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(raw), "first.example") || strings.Contains(string(raw), "second.example") {
			t.Fatal("expired data segment was not reclaimed")
		}
	}
}

func TestRecordLimitReclaimsDiskAndSurvivesRestart(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	dir := t.TempDir()
	path := filepath.Join(dir, "singbox.log")
	j := New(filepath.Join(dir, "dns"))
	writeLog(t, path, source(now, "1", "192.0.2.10"))
	if err := j.Scan(path, true, now); err != nil {
		t.Fatal(err)
	}
	// 验证开启后的连续查询，不把首次尾部接入当作完整历史回填。
	writeLog(t, path, strings.Repeat(query(now, "1", "ads.example.", "A"), MaxRecords+1))
	for i := 0; i < 5; i++ {
		if err := j.Scan(path, true, now); err != nil {
			t.Fatal(err)
		}
	}
	r := j.Search(Filter{Limit: 1}, now)
	if r.Total == 0 || r.Total > MaxRecords {
		t.Fatal("history was not bounded", r.Total)
	}
	diskCount := 0
	for _, count := range j.segmentRecords {
		diskCount += count
	}
	if diskCount > MaxRecords {
		t.Fatal("excess records only hidden in memory", diskCount)
	}
	reopened := New(j.dir)
	if err := reopened.Scan(path, true, now); err != nil {
		t.Fatal(err)
	}
	if reopened.Search(Filter{Limit: 1}, now).Total != r.Total {
		t.Fatal("restart resurrected discarded records")
	}
}
