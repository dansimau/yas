package yas

import (
	"encoding/json"
	"os"
	"path"
	"sync"
	"time"

	"github.com/dansimau/yas/pkg/fsutil"
)

type yasData struct {
	Branches *branchMap `json:"branches"`
}
type yasDatabase struct {
	*yasData

	filePath string
}

func (d *yasDatabase) Save() error {
	b, err := json.MarshalIndent(d.yasData, "", "  ")
	if err != nil {
		return err
	}

	// Ensure the directory exists
	if err := os.MkdirAll(path.Dir(d.filePath), 0o755); err != nil {
		return err
	}

	return os.WriteFile(d.filePath, b, 0o644)
}

// Reload re-reads the database from disk, refreshing the in-memory data.
func (d *yasDatabase) Reload() error {
	exists, err := fsutil.FileExists(d.filePath)
	if err != nil {
		return err
	}

	if !exists {
		// Reset to empty state if file doesn't exist
		d.Branches.Lock()
		d.Branches.data = map[string]BranchMetadata{}
		d.Branches.Unlock()

		return nil
	}

	b, err := os.ReadFile(d.filePath)
	if err != nil {
		return err
	}

	newData := &yasData{
		Branches: &branchMap{
			data: map[string]BranchMetadata{},
		},
	}

	if err := json.Unmarshal(b, newData); err != nil {
		return err
	}

	// Update the in-memory data with a lock
	d.Branches.Lock()
	d.Branches.data = newData.Branches.data
	d.Branches.Unlock()

	return nil
}

// migrateCreatedTimestamps backfills Created timestamps for branches that don't have them.
// This ensures consistent ordering across runs. The migration is saved to disk immediately.
func (d *yasDatabase) migrateCreatedTimestamps() error {
	d.Branches.Lock()

	needsSave := false
	now := time.Now()

	for name, bm := range d.Branches.data {
		if bm.Created.IsZero() && name != "" {
			bm.Created = now
			d.Branches.data[name] = bm
			needsSave = true
		}
	}

	d.Branches.Unlock()

	if needsSave {
		return d.Save()
	}

	return nil
}

func loadData(filePath string) (*yasDatabase, error) {
	db := &yasDatabase{
		filePath: filePath,
		yasData: &yasData{
			Branches: &branchMap{
				data: map[string]BranchMetadata{},
			},
		},
	}

	exists, err := fsutil.FileExists(filePath)
	if err != nil {
		return nil, err
	}

	if !exists {
		return db, nil
	}

	b, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal(b, &db.yasData); err != nil {
		return nil, err
	}

	// Migrate: Backfill Created timestamps for branches that don't have them
	if err := db.migrateCreatedTimestamps(); err != nil {
		return nil, err
	}

	return db, nil
}

type branchMap struct {
	sync.RWMutex
	data map[string]BranchMetadata
}

func (m *branchMap) Exists(name string) bool {
	m.RLock()
	defer m.RUnlock()

	_, exists := m.data[name]

	return exists
}

func (m *branchMap) Get(name string) BranchMetadata {
	m.RLock()
	defer m.RUnlock()

	bm := m.data[name]
	bm.Name = name

	return bm
}

func (m *branchMap) Remove(name string) {
	m.Lock()
	defer m.Unlock()

	delete(m.data, name)
}

func (m *branchMap) Set(name string, data BranchMetadata) {
	m.Lock()
	defer m.Unlock()

	m.data[name] = data
}

func (m *branchMap) ToSlice() (bm Branches) {
	m.RLock()
	defer m.RUnlock()

	for _, branchMetadata := range m.data {
		bm = append(bm, branchMetadata)
	}

	return bm
}

func (m *branchMap) MarshalJSON() ([]byte, error) {
	m.RLock()
	defer m.RUnlock()

	return json.Marshal(m.data)
}

func (m *branchMap) UnmarshalJSON(data []byte) error {
	m.Lock()
	defer m.Unlock()

	return json.Unmarshal(data, &m.data)
}
