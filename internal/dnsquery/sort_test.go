package dnsquery

import (
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
