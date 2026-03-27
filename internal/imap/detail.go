package imap

import (
	"github.com/emersion/go-imap/v2"
)

// DetailLevel controls the verbosity of message fetching.
type DetailLevel string

const (
	DetailHeaders DetailLevel = "headers"
	DetailFull    DetailLevel = "full"
)

// ParseDetailLevel parses a detail level string, defaulting to the given fallback.
func ParseDetailLevel(s string, fallback DetailLevel) DetailLevel {
	switch DetailLevel(s) {
	case DetailHeaders, DetailFull:
		return DetailLevel(s)
	default:
		return fallback
	}
}

// MinimalMessage is a lightweight message representation for minimal detail level.
type MinimalMessage struct {
	UID     uint32 `json:"uid"`
	From    string `json:"from"`
	Subject string `json:"subject"`
	Date    string `json:"date"`
}

// UIDSetFromUID creates a UIDSet containing a single UID.
func UIDSetFromUID(uid uint32) imap.UIDSet {
	s := imap.UIDSet{}
	s.AddNum(imap.UID(uid))
	return s
}

// UIDSetFromUIDs creates a UIDSet from a slice of UIDs.
func UIDSetFromUIDs(uids []uint32) imap.UIDSet {
	return buildUIDSet(uids)
}

// FetchOptionsByLevel returns IMAP fetch options appropriate for the detail level.
func FetchOptionsByLevel(level DetailLevel) *imap.FetchOptions {
	switch level {
	case DetailHeaders:
		return &imap.FetchOptions{
			Envelope: true,
			UID:      true,
		}
	default: // DetailFull
		return &imap.FetchOptions{
			Envelope:   true,
			UID:        true,
			Flags:      true,
			RFC822Size: true,
			BodySection: []*imap.FetchItemBodySection{
				{Peek: true},
			},
		}
	}
}
