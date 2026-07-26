package csvstore

import (
	"os"
	"testing"
)

// writeTempCSV writes content to a temporary CSV file and returns its path.
func writeTempCSV(t *testing.T, content string) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "players-*.csv")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(content); err != nil {
		t.Fatal(err)
	}
	f.Close()
	return f.Name()
}

func TestStore_PlayerIDs(t *testing.T) {
	t.Run("valid CSV returns player IDs in file order", func(t *testing.T) {
		store := New(writeTempCSV(t, "player1,discord1\nplayer2,discord2\n"))
		ids, err := store.PlayerIDs()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := []string{"player1", "player2"}
		if len(ids) != len(want) {
			t.Fatalf("got %d IDs, want %d: %v", len(ids), len(want), ids)
		}
		for i, id := range ids {
			if id != want[i] {
				t.Errorf("ids[%d] = %q, want %q", i, id, want[i])
			}
		}
	})

	t.Run("empty file returns empty slice", func(t *testing.T) {
		store := New(writeTempCSV(t, ""))
		ids, err := store.PlayerIDs()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(ids) != 0 {
			t.Errorf("expected empty slice, got %v", ids)
		}
	})

	t.Run("non-existent file is created and returns empty slice", func(t *testing.T) {
		store := New(t.TempDir() + "/new-players.csv")
		ids, err := store.PlayerIDs()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(ids) != 0 {
			t.Errorf("expected empty slice, got %v", ids)
		}
	})
}

func TestStore_FindByPlayerID(t *testing.T) {
	store := New(writeTempCSV(t, "player1,discord1\nplayer2,discord2\n"))

	t.Run("found", func(t *testing.T) {
		externalID, found, err := store.FindByPlayerID("player1")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !found {
			t.Fatal("expected found=true")
		}
		if externalID != "discord1" {
			t.Errorf("externalID = %q, want %q", externalID, "discord1")
		}
	})

	t.Run("not found", func(t *testing.T) {
		_, found, err := store.FindByPlayerID("unknown")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if found {
			t.Fatal("expected found=false")
		}
	})
}

func TestStore_AddPlayer(t *testing.T) {
	store := New(writeTempCSV(t, "player1,discord1\n"))

	if err := store.AddPlayer("player2", "discord2"); err != nil {
		t.Fatalf("AddPlayer failed: %v", err)
	}

	ids, err := store.PlayerIDs()
	if err != nil {
		t.Fatalf("unexpected error reading IDs: %v", err)
	}
	if len(ids) != 2 {
		t.Fatalf("expected 2 IDs, got %d: %v", len(ids), ids)
	}

	externalID, found, err := store.FindByPlayerID("player2")
	if err != nil {
		t.Fatalf("unexpected error in FindByPlayerID: %v", err)
	}
	if !found {
		t.Fatal("expected player2 to be found after AddPlayer")
	}
	if externalID != "discord2" {
		t.Errorf("externalID = %q, want %q", externalID, "discord2")
	}
}

func TestStore_AddPlayer_doesNotDuplicate(t *testing.T) {
	store := New(writeTempCSV(t, ""))

	if err := store.AddPlayer("player1", "discord1"); err != nil {
		t.Fatalf("first AddPlayer failed: %v", err)
	}

	// AddPlayer does not deduplicate — the service layer always calls
	// FindByPlayerID first and only calls AddPlayer for new players.
	// This test confirms AddPlayer faithfully writes what it is given.
	if err := store.AddPlayer("player1", "discord1b"); err != nil {
		t.Fatalf("second AddPlayer failed: %v", err)
	}

	ids, err := store.PlayerIDs()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Two rows — deduplication is the caller's responsibility.
	if len(ids) != 2 {
		t.Fatalf("expected 2 rows, got %d: %v", len(ids), ids)
	}
}
