// Package backup exports the whole server into one archive and imports it
// back, in full or by sections. The export always carries everything; the
// import asks what to take and what to do on conflicts.
//
// Archive layout (tar.gz):
//
//	manifest.json  Format, panel version, time, sections with counts and hashes
//	database.db    a consistent copy of the whole database
//	files/...      panel files outside the database (TLS material)
//
// Admins never import (an archive must not mint panel access) and neither do
// sessions or the audit trail.
package backup

import (
	"errors"
	"fmt"
)

// Format is the archive version this code reads and writes.
const Format = 1

// Sections in dependency order: tariffs before users, users before nothing else
// (devices and counters follow their user), nodes before inbounds.
const (
	SectionUsers    = "users"
	SectionInbounds = "inbounds"
	SectionNodes    = "nodes"
	SectionNetwork  = "network"
	SectionTariffs  = "tariffs"
	SectionSettings = "settings"
)

// Sections lists every importable section, in dependency order.
func Sections() []string {
	return []string{SectionTariffs, SectionNodes, SectionInbounds, SectionUsers, SectionNetwork, SectionSettings}
}

// Strategy says what to do with a row the target already has (matched by its
// natural key): skip it, overwrite it, or store the incoming one as new (fresh
// tokens and uuids; handed-out links die).
type Strategy string

const (
	StrategySkip    Strategy = "skip"
	StrategyReplace Strategy = "replace"
	StrategyFresh   Strategy = "fresh"
)

// File names inside the archive.
const (
	ManifestName = "manifest.json"
	DatabaseName = "database.db"
	FilesPrefix  = "files/"
)

// MaxArchive is the largest upload the panel takes: streaming, with progress.
const MaxArchive = 2 << 30 // 2 GB

// SectionInfo is one section of a manifest: what is inside and its hash.
type SectionInfo struct {
	Count  int    `json:"count"`
	SHA256 string `json:"sha256,omitempty"`
}

// Manifest describes an archive.
type Manifest struct {
	Format         int                    `json:"format"`
	Panel          string                 `json:"panel"`
	Created        int64                  `json:"created_at"`
	DatabaseSHA256 string                 `json:"database_sha256"`
	Sections       map[string]SectionInfo `json:"sections"`
}

// Overlap counts, per section, how many incoming rows already exist locally.
type Overlap map[string]int

var (
	ErrFormat      = errors.New("not a cozy backup")
	ErrTooBig      = errors.New("backup too big")
	ErrSlip        = errors.New("backup path escapes")
	ErrNoSections  = errors.New("no sections selected")
	ErrNeedSection = errors.New("unknown section")
)

func checkStrategy(s Strategy) error {
	switch s {
	case StrategySkip, StrategyReplace, StrategyFresh:
		return nil
	}
	return fmt.Errorf("bad strategy %q", s)
}
