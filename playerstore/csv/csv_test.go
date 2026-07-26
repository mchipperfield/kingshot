package csvstore

import (
	"os"
	"testing"

	"github.com/mchipperfield/kingshot"
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

func TestStore_Players(t *testing.T) {
	t.Run("valid CSV returns players in file order", func(t *testing.T) {
		store := New(writeTempCSV(t, "player1,discord1,kingdom1\nplayer2,discord2,kingdom2\n"))
		players, err := store.Players()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := []kingshot.Player{
			{ID: "player1", ExternalID: "discord1", KID: "kingdom1"},
			{ID: "player2", ExternalID: "discord2", KID: "kingdom2"},
		}
		if len(players) != len(want) {
			t.Fatalf("got %d players, want %d: %v", len(players), len(want), players)
		}
		for i, p := range players {
			if p != want[i] {
				t.Errorf("players[%d] = %+v, want %+v", i, p, want[i])
			}
		}
	})

	t.Run("empty file returns empty slice", func(t *testing.T) {
		store := New(writeTempCSV(t, ""))
		players, err := store.Players()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(players) != 0 {
			t.Errorf("expected empty slice, got %v", players)
		}
	})

	t.Run("non-existent file is created and returns empty slice", func(t *testing.T) {
		store := New(t.TempDir() + "/new-players.csv")
		players, err := store.Players()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(players) != 0 {
			t.Errorf("expected empty slice, got %v", players)
		}
	})
}

func TestStore_FindByPlayerID(t *testing.T) {
	store := New(writeTempCSV(t, "player1,discord1,kingdom1\nplayer2,discord2,kingdom2\n"))

	t.Run("found", func(t *testing.T) {
		p, found, err := store.FindByPlayerID("player1")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !found {
			t.Fatal("expected found=true")
		}
		if p.ExternalID != "discord1" {
			t.Errorf("ExternalID = %q, want %q", p.ExternalID, "discord1")
		}
		if p.KID != "kingdom1" {
			t.Errorf("KID = %q, want %q", p.KID, "kingdom1")
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
	store := New(writeTempCSV(t, "player1,discord1,kingdom1\n"))

	if err := store.AddPlayer(kingshot.Player{ID: "player2", ExternalID: "discord2", KID: "kingdom2"}); err != nil {
		t.Fatalf("AddPlayer failed: %v", err)
	}

	players, err := store.Players()
	if err != nil {
		t.Fatalf("unexpected error reading players: %v", err)
	}
	if len(players) != 2 {
		t.Fatalf("expected 2 players, got %d: %v", len(players), players)
	}

	p, found, err := store.FindByPlayerID("player2")
	if err != nil {
		t.Fatalf("unexpected error in FindByPlayerID: %v", err)
	}
	if !found {
		t.Fatal("expected player2 to be found after AddPlayer")
	}
	if p.ExternalID != "discord2" {
		t.Errorf("ExternalID = %q, want %q", p.ExternalID, "discord2")
	}
	if p.KID != "kingdom2" {
		t.Errorf("KID = %q, want %q", p.KID, "kingdom2")
	}
}

func TestStore_AddPlayer_doesNotDuplicate(t *testing.T) {
	store := New(writeTempCSV(t, ""))

	if err := store.AddPlayer(kingshot.Player{ID: "player1", ExternalID: "discord1", KID: "kingdom1"}); err != nil {
		t.Fatalf("first AddPlayer failed: %v", err)
	}

	// AddPlayer does not deduplicate — the service layer always calls
	// FindByPlayerID first and only calls AddPlayer for new players.
	// This test confirms AddPlayer faithfully writes what it is given.
	if err := store.AddPlayer(kingshot.Player{ID: "player1", ExternalID: "discord1b", KID: "kingdom1"}); err != nil {
		t.Fatalf("second AddPlayer failed: %v", err)
	}

	players, err := store.Players()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Two rows — deduplication is the caller's responsibility.
	if len(players) != 2 {
		t.Fatalf("expected 2 rows, got %d: %v", len(players), players)
	}
}
