package yas

import (
	"encoding/json"
	"os"
	"sync"

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

	return os.WriteFile(d.filePath, b, 0o644)
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

	if !fsutil.FileExists(filePath) {
		return db, nil
	}

	b, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal(b, &db.yasData); err != nil {
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
