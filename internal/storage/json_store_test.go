package storage

import "testing"

func TestJSONStoreNodeQueriesSkipDisabledNodes(t *testing.T) {
	store, err := NewJSONStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewJSONStore() error = %v", err)
	}

	if err := store.AddSubscription(Subscription{
		ID:      "sub-enabled",
		Name:    "enabled-sub",
		Enabled: true,
		Nodes: []Node{
			{Tag: "sub-enabled-hk", Country: "HK"},
			{Tag: "sub-disabled-us", Country: "US", Disabled: true},
			{Tag: "sub-enabled-us", Country: "US"},
		},
	}); err != nil {
		t.Fatalf("AddSubscription(enabled) error = %v", err)
	}

	if err := store.AddSubscription(Subscription{
		ID:      "sub-disabled",
		Name:    "disabled-sub",
		Enabled: false,
		Nodes: []Node{
			{Tag: "sub-offline-jp", Country: "JP"},
		},
	}); err != nil {
		t.Fatalf("AddSubscription(disabled) error = %v", err)
	}

	if err := store.AddManualNode(ManualNode{
		ID:      "manual-enabled",
		Enabled: true,
		Node:    Node{Tag: "manual-enabled-us", Country: "US"},
	}); err != nil {
		t.Fatalf("AddManualNode(enabled) error = %v", err)
	}

	if err := store.AddManualNode(ManualNode{
		ID:      "manual-disabled",
		Enabled: false,
		Node:    Node{Tag: "manual-disabled-hk", Country: "HK"},
	}); err != nil {
		t.Fatalf("AddManualNode(disabled) error = %v", err)
	}

	t.Run("GetAllNodes", func(t *testing.T) {
		nodes := store.GetAllNodes()
		wantTags := []string{"sub-enabled-hk", "sub-enabled-us", "manual-enabled-us"}

		if len(nodes) != len(wantTags) {
			t.Fatalf("len(GetAllNodes()) = %d, want %d", len(nodes), len(wantTags))
		}

		for idx, want := range wantTags {
			if nodes[idx].Tag != want {
				t.Fatalf("GetAllNodes()[%d].Tag = %q, want %q", idx, nodes[idx].Tag, want)
			}
		}
	})

	t.Run("GetAllNodesPtr", func(t *testing.T) {
		nodes := store.GetAllNodesPtr()
		wantTags := []string{"sub-enabled-hk", "sub-enabled-us", "manual-enabled-us"}

		if len(nodes) != len(wantTags) {
			t.Fatalf("len(GetAllNodesPtr()) = %d, want %d", len(nodes), len(wantTags))
		}

		for idx, want := range wantTags {
			if nodes[idx].Tag != want {
				t.Fatalf("GetAllNodesPtr()[%d].Tag = %q, want %q", idx, nodes[idx].Tag, want)
			}
			if nodes[idx].Disabled {
				t.Fatalf("GetAllNodesPtr()[%d] should not be disabled", idx)
			}
		}
	})

	t.Run("GetNodesByCountry", func(t *testing.T) {
		nodes := store.GetNodesByCountry("US")
		wantTags := []string{"sub-enabled-us", "manual-enabled-us"}

		if len(nodes) != len(wantTags) {
			t.Fatalf("len(GetNodesByCountry(US)) = %d, want %d", len(nodes), len(wantTags))
		}

		for idx, want := range wantTags {
			if nodes[idx].Tag != want {
				t.Fatalf("GetNodesByCountry()[%d].Tag = %q, want %q", idx, nodes[idx].Tag, want)
			}
		}
	})

	t.Run("GetCountryGroups", func(t *testing.T) {
		groups := store.GetCountryGroups()
		counts := make(map[string]int, len(groups))
		for _, group := range groups {
			counts[group.Code] = group.NodeCount
		}

		if counts["US"] != 2 {
			t.Fatalf("country US count = %d, want 2", counts["US"])
		}
		if counts["HK"] != 1 {
			t.Fatalf("country HK count = %d, want 1", counts["HK"])
		}
		if _, ok := counts["JP"]; ok {
			t.Fatalf("country JP should be excluded from disabled subscription")
		}
	})
}

func TestJSONStoreSubscriptionNodeCountTracksEnabledNodes(t *testing.T) {
	store, err := NewJSONStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewJSONStore() error = %v", err)
	}

	if err := store.AddSubscription(Subscription{
		ID:      "sub-count",
		Name:    "sub-count",
		Enabled: true,
		Nodes: []Node{
			{Tag: "a", Country: "HK"},
			{Tag: "b", Country: "US", Disabled: true},
			{Tag: "c", Country: "JP"},
		},
	}); err != nil {
		t.Fatalf("AddSubscription() error = %v", err)
	}

	initialSub := store.GetSubscription("sub-count")
	if initialSub == nil {
		t.Fatalf("subscription should exist")
	}
	if initialSub.NodeCount != 0 {
		t.Fatalf("initial NodeCount = %d, want 0 before update", initialSub.NodeCount)
	}

	if err := store.UpdateSubscription(*initialSub); err != nil {
		t.Fatalf("UpdateSubscription() error = %v", err)
	}

	updatedSub := store.GetSubscription("sub-count")
	if updatedSub == nil {
		t.Fatalf("updated subscription should exist")
	}
	if updatedSub.NodeCount != 2 {
		t.Fatalf("NodeCount after UpdateSubscription = %d, want 2", updatedSub.NodeCount)
	}

	nodes := []Node{
		{Tag: "d", Country: "SG", Disabled: true},
		{Tag: "e", Country: "SG"},
	}
	if err := store.SaveSubscriptionNodes("sub-count", nodes); err != nil {
		t.Fatalf("SaveSubscriptionNodes() error = %v", err)
	}

	finalSub := store.GetSubscription("sub-count")
	if finalSub == nil {
		t.Fatalf("final subscription should exist")
	}
	if finalSub.NodeCount != 1 {
		t.Fatalf("NodeCount after SaveSubscriptionNodes = %d, want 1", finalSub.NodeCount)
	}
}
