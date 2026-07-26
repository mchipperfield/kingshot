// Package csvstore provides a CSV-backed implementation of kingshot.PlayerStore.
// Each row in the file is stored as "playerID,externalID,kid".
package csvstore

import (
	"encoding/csv"
	"io"
	"os"

	"github.com/mchipperfield/kingshot"
)

// Store is a CSV-backed PlayerStore. It is not safe for concurrent use without
// external synchronisation; the owning GiftCodeService serialises all calls via
// its mutex.
type Store struct {
	path string
}

// New returns a Store backed by the CSV file at path. The file is created on
// first write if it does not already exist.
func New(path string) *Store {
	return &Store{path: path}
}

// Players returns all registered players in file order.
func (s *Store) Players() ([]kingshot.Player, error) {
	records, err := s.readAll()
	if err != nil {
		return nil, err
	}
	players := make([]kingshot.Player, 0, len(records))
	for _, record := range records {
		if len(record) >= 3 {
			players = append(players, kingshot.Player{
				ID:         record[0],
				ExternalID: record[1],
				KID:        record[2],
			})
		}
	}
	return players, nil
}

// FindByPlayerID looks up the player record for playerID.
// found is false if the player is not registered.
func (s *Store) FindByPlayerID(playerID string) (kingshot.Player, bool, error) {
	records, err := s.readAll()
	if err != nil {
		return kingshot.Player{}, false, err
	}
	for _, record := range records {
		if len(record) >= 3 && record[0] == playerID {
			return kingshot.Player{
				ID:         record[0],
				ExternalID: record[1],
				KID:        record[2],
			}, true, nil
		}
	}
	return kingshot.Player{}, false, nil
}

// AddPlayer appends a new player record to the file.
// The caller (GiftCodeService) is responsible for calling FindByPlayerID first
// to ensure a playerID is not registered more than once.
func (s *Store) AddPlayer(player kingshot.Player) error {
	records, err := s.readAll()
	if err != nil {
		return err
	}
	records = append(records, []string{player.ID, player.ExternalID, player.KID})
	return s.writeAll(records)
}

// readAll opens the file (creating it if absent) and returns all CSV records.
func (s *Store) readAll() ([][]string, error) {
	file, err := os.OpenFile(s.path, os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	reader := csv.NewReader(file)
	records, err := reader.ReadAll()
	if err != nil && err != io.EOF {
		return nil, err
	}
	return records, nil
}

// writeAll rewrites the file with the given records.
func (s *Store) writeAll(records [][]string) error {
	file, err := os.OpenFile(s.path, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	defer file.Close()

	w := csv.NewWriter(file)
	for _, record := range records {
		if err := w.Write(record); err != nil {
			return err
		}
	}
	w.Flush()
	return w.Error()
}
