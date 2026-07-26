// Package csvstore provides a CSV-backed implementation of kingshot.PlayerStore.
// Each row in the file is stored as "playerID,externalID".
package csvstore

import (
	"encoding/csv"
	"io"
	"os"
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

// PlayerIDs returns all registered player IDs in file order.
func (s *Store) PlayerIDs() ([]string, error) {
	records, err := s.readAll()
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(records))
	for _, record := range records {
		if len(record) >= 1 {
			ids = append(ids, record[0])
		}
	}
	return ids, nil
}

// FindByPlayerID looks up the external ID for playerID.
// found is false if the player is not registered.
func (s *Store) FindByPlayerID(playerID string) (externalID string, found bool, err error) {
	records, err := s.readAll()
	if err != nil {
		return "", false, err
	}
	for _, record := range records {
		if len(record) >= 2 && record[0] == playerID {
			return record[1], true, nil
		}
	}
	return "", false, nil
}

// AddPlayer appends a new playerID → externalID row to the file.
// The caller (GiftCodeService) is responsible for calling FindByPlayerID first
// to ensure a playerID is not registered more than once.
func (s *Store) AddPlayer(playerID, externalID string) error {
	records, err := s.readAll()
	if err != nil {
		return err
	}
	records = append(records, []string{playerID, externalID})
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
