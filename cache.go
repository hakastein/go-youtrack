package youtrack

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
)

const (
	cacheDirectoryMode fs.FileMode = 0o700
	cacheFileMode      fs.FileMode = 0o600
)

// The cache confirms and never refuses: whatever disagrees with the server is a miss, and the metadata is read again.
type metaCache struct {
	directory string
}

func newMetaCache(root, address, token string) metaCache {
	if root == "" {
		return metaCache{}
	}
	login := sha256.Sum256([]byte(address + "\x00" + token))
	return metaCache{directory: filepath.Join(root, hex.EncodeToString(login[:]))}
}

type cachedField struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	LocalizedName string `json:"localizedName"`
	ValueType     string `json:"valueType"`
	IsMultiValue  bool   `json:"isMultiValue"`
	CanBeEmpty    bool   `json:"canBeEmpty"`
}

func (c metaCache) load(target string) ([]ProjectField, bool) {
	path := c.file(target)
	if path == "" {
		return nil, false
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	var held []cachedField
	if json.Unmarshal(content, &held) != nil || len(held) == 0 {
		return nil, false
	}
	fields := make([]ProjectField, 0, len(held))
	for _, f := range held {
		fields = append(fields, ProjectField{ID: f.ID, Name: f.Name, LocalizedName: f.LocalizedName,
			Type: FieldType{ValueType: ValueType(f.ValueType), Multi: f.IsMultiValue}, CanBeEmpty: f.CanBeEmpty})
	}
	return fields, true
}

func (c metaCache) store(target string, fields []ProjectField) {
	path := c.file(target)
	if path == "" {
		return
	}
	held := make([]cachedField, 0, len(fields))
	for _, f := range fields {
		held = append(held, cachedField{ID: f.ID, Name: f.Name, LocalizedName: f.LocalizedName, ValueType: string(f.Type.ValueType),
			IsMultiValue: f.Type.Multi, CanBeEmpty: f.CanBeEmpty})
	}
	content, _ := json.Marshal(held)
	_ = c.write(path, content)
}

func (c metaCache) write(path string, content []byte) (err error) {
	if err = os.MkdirAll(c.directory, cacheDirectoryMode); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(c.directory, ".metadata-*")
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = temporary.Close()
			_ = os.Remove(temporary.Name())
		}
	}()
	if _, err = temporary.Write(content); err != nil {
		return err
	}
	// The umask may narrow the 0600 os.CreateTemp uses.
	if err = temporary.Chmod(cacheFileMode); err != nil {
		return err
	}
	if err = temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporary.Name(), path)
}

func (c metaCache) file(target string) string {
	if c.directory == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(target))
	return filepath.Join(c.directory, hex.EncodeToString(sum[:]))
}
