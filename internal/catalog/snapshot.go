package catalog

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
)

type Object map[string]any
type Snapshot struct {
	Version, Hash string
	Metadata      Object
	Entries       map[int64]Object
	NPCs          []Object
}
type Bundle struct{ Client, Server, Monsters, NPCs Snapshot }
type Paths struct{ Client, Server, Monsters, NPCs string }
type SnapshotReader interface{ Read(Paths) (Bundle, error) }
type JSONReader struct{}

func decode(raw []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	return decoder.Decode(target)
}
func readDocument(path string) (Object, string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, "", err
	}
	defer file.Close()
	const limit = 128 << 20
	raw, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, "", err
	}
	if len(raw) > limit {
		return nil, "", errors.New("catalog exceeds 128 MiB")
	}
	var doc Object
	// Unmarshal checks trailing JSON, while the decoder retains integer precision.
	if !json.Valid(raw) {
		return nil, "", fmt.Errorf("invalid catalog JSON: %s", path)
	}
	if err = decode(raw, &doc); err != nil || doc == nil {
		return nil, "", fmt.Errorf("catalog must be an object: %s", path)
	}
	hash := sha256.Sum256(raw)
	return doc, hex.EncodeToString(hash[:]), nil
}
func object(value any) Object {
	switch v := value.(type) {
	case map[string]any:
		return Object(v)
	case Object:
		return v
	}
	return nil
}
func text(value any) string { valueString, _ := value.(string); return valueString }
func integer(value any) (int64, bool) {
	switch n := value.(type) {
	case json.Number:
		i, err := n.Int64()
		return i, err == nil
	case int:
		return int64(n), true
	case int64:
		return n, true
	}
	return 0, false
}
func number(value any, otherwise int64) int64 {
	if n, ok := integer(value); ok {
		return n
	}
	return otherwise
}
func boolValue(value any, otherwise bool) bool {
	if b, ok := value.(bool); ok {
		return b
	}
	return otherwise
}

func readSnapshot(path, kind string) (Snapshot, error) {
	doc, hash, err := readDocument(path)
	if err != nil {
		return Snapshot{}, err
	}
	snapshot := Snapshot{Hash: hash, Metadata: doc}
	field := ""
	switch kind {
	case "client", "server":
		if text(doc["schema"]) != "item-catalog/v2" {
			return snapshot, errors.New("unsupported item catalog schema")
		}
		if kind == "client" {
			if text(doc["mode"]) != "client-only" {
				return snapshot, errors.New("client item catalog must be client-only")
			}
			snapshot.Version = text(object(doc["clientSource"])["version"])
		} else {
			if text(doc["mode"]) != "renewal" {
				return snapshot, errors.New("server item catalog must use Renewal")
			}
			snapshot.Version = text(object(doc["englishSource"])["revision"])
		}
		field = "items"
	case "monsters":
		if text(doc["schema"]) != "monster-catalog/v1" {
			return snapshot, errors.New("unsupported monster catalog schema")
		}
		snapshot.Version = text(object(doc["serverSource"])["revision"])
		field = "monsters"
	case "npcs":
		if text(doc["schema"]) != "happyro-npc-catalog/v1" {
			return snapshot, errors.New("unsupported NPC catalog schema")
		}
		snapshot.Version = text(doc["version"])
		field = "entries"
	default:
		return snapshot, errors.New("unknown catalog type")
	}
	if snapshot.Version == "" || len(snapshot.Version) > 128 {
		return snapshot, errors.New("catalog source version is missing or too long")
	}
	if kind == "npcs" {
		entries, ok := doc[field].([]any)
		if !ok {
			return snapshot, errors.New("NPC entries must be an array")
		}
		seen := map[string]bool{}
		for _, value := range entries {
			entry := object(value)
			key := text(entry["id"])
			if key == "" || len(key) > 255 || seen[key] || text(entry["map"]) == "" {
				return snapshot, errors.New("invalid or duplicate NPC key")
			}
			seen[key] = true
			snapshot.NPCs = append(snapshot.NPCs, entry)
		}
	} else {
		entries := object(doc[field])
		if len(entries) == 0 {
			return snapshot, errors.New("catalog entries must be a nonempty object")
		}
		snapshot.Entries = map[int64]Object{}
		for key, value := range entries {
			id, err := strconv.ParseInt(key, 10, 32)
			entry := object(value)
			names := object(entry["names"])
			if err != nil || id <= 0 || strconv.FormatInt(id, 10) != key || entry == nil || text(names["zh-CN"]) == "" || text(names["en-US"]) == "" {
				return snapshot, fmt.Errorf("invalid %s catalog entry %q", kind, key)
			}
			if rawID, present := entry["Id"]; present {
				actual, valid := integer(rawID)
				if !valid || actual != id {
					return snapshot, fmt.Errorf("catalog ID mismatch: %s", key)
				}
			}
			snapshot.Entries[id] = entry
		}
	}
	delete(doc, field)
	return snapshot, nil
}
func (JSONReader) Read(paths Paths) (Bundle, error) {
	var bundle Bundle
	for _, item := range []struct {
		path, kind string
		target     *Snapshot
	}{{paths.Client, "client", &bundle.Client}, {paths.Server, "server", &bundle.Server}, {paths.Monsters, "monsters", &bundle.Monsters}, {paths.NPCs, "npcs", &bundle.NPCs}} {
		value, err := readSnapshot(item.path, item.kind)
		if err != nil {
			return Bundle{}, fmt.Errorf("read %s catalog: %w", item.kind, err)
		}
		*item.target = value
	}
	if bundle.Server.Version != bundle.Monsters.Version {
		return Bundle{}, errors.New("item and monster server revisions must match")
	}
	return bundle, nil
}
func (b Bundle) Hash() string {
	sum := sha256.Sum256([]byte("catalog-projection/v1:" + b.Client.Hash + ":" + b.Server.Hash + ":" + b.Monsters.Hash + ":" + b.NPCs.Hash))
	return hex.EncodeToString(sum[:])
}
