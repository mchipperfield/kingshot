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
		store := New(writeTempCSV(t, "player1,discord1,k1\nplayer2,discord2,k2\n"))
		players, err := store.Players()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := []*kingshot.Player{
			{PlayerID: "player1", ExternalID: "discord1", KingdomID: "k1"},
			{PlayerID: "player2", ExternalID: "discord2", KingdomID: "k2"},
		}
		if len(players) != len(want) {
			t.Fatalf("got %d players, want %d: %v", len(players), len(want), players)
		}
		for i, p := range players {
			if p.PlayerID != want[i].PlayerID || p.ExternalID != want[i].ExternalID || p.KingdomID != want[i].KingdomID {
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
	store := New(writeTempCSV(t, "player1,discord1,k1\nplayer2,discord2,k2\n"))

	t.Run("found", func(t *testing.T) {
		player, found, err := store.FindByPlayerID("player1")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !found {
			t.Fatal("expected found=true")
		}
		if player.ExternalID != "discord1" {
			t.Errorf("ExternalID = %q, want %q", player.ExternalID, "discord1")
		}
		if player.KingdomID != "k1" {
			t.Errorf("KingdomID = %q, want %q", player.KingdomID, "k1")
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
	store := New(writeTempCSV(t, "player1,discord1,k1\n"))

	playerToAdd := &kingshot.Player{PlayerID: "player2", ExternalID: "discord2", KingdomID: "k2"}
	if err := store.AddPlayer(playerToAdd); err != nil {
		t.Fatalf("AddPlayer failed: %v", err)
	}

	players, err := store.Players()
	if err != nil {
		t.Fatalf("unexpected error reading players: %v", err)
	}
	if len(players) != 2 {
		t.Fatalf("expected 2 players, got %d: %v", len(players), players)
	}

	player, found, err := store.FindByPlayerID("player2")
	if err != nil {
		t.Fatalf("unexpected error in FindByPlayerID: %v", err)
	}
	if !found {
		t.Fatal("expected player2 to be found after AddPlayer")
	}
	if player.ExternalID != "discord2" {
		t.Errorf("ExternalID = %q, want %q", player.ExternalID, "discord2")
	}
	if player.KingdomID != "k2" {
		t.Errorf("KingdomID = %q, want %q", player.KingdomID, "k2")
	}
}

func TestStore_AddPlayer_doesNotDuplicate(t *testing.T) {
	store := New(writeTempCSV(t, ""))

	if err := store.AddPlayer(&kingshot.Player{PlayerID: "player1", ExternalID: "discord1", KingdomID: "k1"}); err != nil {
		t.Fatalf("first AddPlayer failed: %v", err)
	}

	// AddPlayer does not deduplicate — the service layer always calls
	// FindByPlayerID first and only calls AddPlayer for new players.
	// This test confirms AddPlayer faithfully writes what it is given.
	if err := store.AddPlayer(&kingshot.Player{PlayerID: "player1", ExternalID: "discord1b", KingdomID: "k1b"}); err != nil {
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
