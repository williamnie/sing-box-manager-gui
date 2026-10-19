package dnsquery

import (
	"fmt"
	"path/filepath"
	"sort"
	"testing"
	"time"
)

func TestRecentDomainSortBeforePagination(t *testing.T) {
	now := time.Now()
	j := New(t.TempDir())
	j.records = []storedRecord{
		{Record: Record{At: now.Add(-time.Minute).UnixMilli(), Domain: "frequent.test", Source: "192.0.2.1", Type: "A"}},
		{Record: Record{At: now.Add(-time.Minute).UnixMilli(), Domain: "frequent.test", Source: "192.0.2.1", Type: "A"}},
		{Record: Record{At: now.UnixMilli(), Domain: "new.test", Source: "192.0.2.1", Type: "A"}},
	}
	if got := j.Search(Filter{Sort: "recent", Limit: 1}, now); got.Domains[0].Domain != "new.test" || got.DomainTotal != 2 {
		t.Fatal(got)
	}
	if got := j.Search(Filter{Sort: "recent", Limit: 1, Offset: 1}, now); got.Domains[0].Domain != "frequent.test" {
		t.Fatal(got)
	}
	if got := j.Search(Filter{Limit: 1}, now); got.Domains[0].Domain != "frequent.test" {
		t.Fatal("default count sort changed", got)
	}
}

func TestSearchCacheInvalidatesOnNewAndExpiredRecords(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	dir := t.TempDir()
	path := filepath.Join(dir, "singbox.log")
	j := New(filepath.Join(dir, "dns"))
	old := now.Add(-RetentionDays*24*time.Hour + time.Minute)
	writeLog(t, path, source(old, "1", "192.0.2.10")+query(old, "1", "old.example.", "A")+query(now, "1", "new.example.", "A"))
	if err := j.Scan(path, true, now); err != nil {
		t.Fatal(err)
	}
	f := Filter{Limit: 50}
	if r := j.Search(f, now); r.Total != 2 {
		t.Fatal(r)
	}
	// 返回值被调用方修改也不能污染缓存。
	r := j.Search(f, now)
	r.Domains[0].Domain = "mutated"
	if j.Search(f, now).Domains[0].Domain == "mutated" {
		t.Fatal("cache shared with caller")
	}
	writeLog(t, path, query(now, "1", "fresh.example.", "A"))
	if err := j.Scan(path, true, now); err != nil {
		t.Fatal(err)
	}
	if r := j.Search(f, now); r.Total != 3 {
		t.Fatal("new record hidden by cache", r)
	}
	if r := j.Search(f, now.Add(2*time.Minute)); r.Total != 2 {
		t.Fatal("expired record served from cache", r)
	}
}

func TestTopDomainsMatchesFullSort(t *testing.T) {
	domains := map[string]Domain{}
	for i := 0; i < 500; i++ {
		d := Domain{Domain: fmt.Sprintf("d%03d.test", i), Count: i % 7, LastSeen: int64(i % 11)}
		domains[d.Domain] = d
	}
	all := make([]Domain, 0, len(domains))
	for _, d := range domains {
		all = append(all, d)
	}
	for _, by := range []string{"", "recent"} {
		sort.Slice(all, func(i, k int) bool { return domainBefore(all[i], all[k], by) })
		for _, page := range [][2]int{{0, 10}, {37, 50}, {490, 50}, {600, 10}} {
			got := topDomains(domains, Filter{Sort: by, Offset: page[0], Limit: page[1]})
			end := min(page[0]+page[1], len(all))
			want := []Domain{}
			if page[0] < len(all) {
				want = all[page[0]:end]
			}
			if fmt.Sprint(got) != fmt.Sprint(want) {
				t.Fatalf("%s %v: got %v want %v", by, page, got, want)
			}
		}
	}
}
