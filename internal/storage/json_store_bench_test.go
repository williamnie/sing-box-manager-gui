package storage

import (
	"fmt"
	"sync/atomic"
	"testing"
	"time"
)

func benchmarkStore(b *testing.B, subCount, nodesPerSub int) *JSONStore {
	b.Helper()

	store, err := NewJSONStore(b.TempDir())
	if err != nil {
		b.Fatalf("NewJSONStore() error = %v", err)
	}
	b.Cleanup(func() {
		_ = store.Close()
	})

	for i := 0; i < subCount; i++ {
		nodes := make([]Node, 0, nodesPerSub)
		for j := 0; j < nodesPerSub; j++ {
			nodes = append(nodes, Node{
				Tag:        fmt.Sprintf("sub-%03d-node-%03d", i, j),
				Type:       "vmess",
				Server:     "example.com",
				ServerPort: 443,
				Country:    "US",
			})
		}

		if err := store.AddSubscription(Subscription{
			ID:      fmt.Sprintf("sub-%03d", i),
			Name:    fmt.Sprintf("sub-%03d", i),
			Enabled: true,
			Nodes:   nodes,
		}); err != nil {
			b.Fatalf("AddSubscription() error = %v", err)
		}
	}

	if err := store.Save(); err != nil {
		b.Fatalf("Save() error = %v", err)
	}

	return store
}

func BenchmarkJSONStoreGetAllNodesPtr(b *testing.B) {
	store := benchmarkStore(b, 80, 20)

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		nodes := store.GetAllNodesPtr()
		if len(nodes) == 0 {
			b.Fatalf("GetAllNodesPtr() returned empty nodes")
		}
	}
}

func BenchmarkJSONStoreUpdateSubscription(b *testing.B) {
	store := benchmarkStore(b, 50, 15)

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		sub := store.GetSubscription("sub-000")
		if sub == nil {
			b.Fatalf("subscription not found")
		}

		sub.UpdatedAt = time.Now()
		if err := store.UpdateSubscription(*sub); err != nil {
			b.Fatalf("UpdateSubscription() error = %v", err)
		}
	}
}

func BenchmarkJSONStoreConcurrentReadWrite(b *testing.B) {
	store := benchmarkStore(b, 120, 20)
	var counter atomic.Uint64

	b.ReportAllocs()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = store.GetAllNodesPtr()

			if counter.Add(1)%20 == 0 {
				sub := store.GetSubscription("sub-000")
				if sub == nil {
					b.Fatalf("subscription not found")
				}
				sub.UpdatedAt = time.Now()
				if err := store.UpdateSubscription(*sub); err != nil {
					b.Fatalf("UpdateSubscription() error = %v", err)
				}
			}
		}
	})
}
