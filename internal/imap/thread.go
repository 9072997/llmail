package imap

import (
	"context"
	"fmt"
	"sort"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
)

// ThreadResult holds the result of a thread retrieval.
type ThreadResult struct {
	Messages []MessageSummary `json:"messages"`
}

// GetThread retrieves all messages in a thread containing the given UID.
// It first attempts the IMAP THREAD command, then falls back to References/In-Reply-To header chasing.
func GetThread(ctx context.Context, c *imapclient.Client, folder string, uid uint32, level DetailLevel) (*ThreadResult, error) {
	selectCmd := c.Select(folder, &imap.SelectOptions{ReadOnly: true})
	if _, err := selectCmd.Wait(); err != nil {
		return nil, fmt.Errorf("selecting folder %q: %w", folder, err)
	}

	// Check if server supports THREAD=REFERENCES
	caps := c.Caps()
	algorithms := caps.ThreadAlgorithms()
	hasThread := false
	for _, alg := range algorithms {
		if alg == imap.ThreadReferences {
			hasThread = true
			break
		}
	}

	if hasThread {
		result, err := getThreadViaIMAP(ctx, c, uid, level)
		if err == nil {
			return result, nil
		}
		// Fall through to references-based approach on error
	}

	return getThreadViaReferences(ctx, c, folder, uid, level)
}

// getThreadViaIMAP uses the IMAP THREAD command to find related messages.
func getThreadViaIMAP(ctx context.Context, c *imapclient.Client, uid uint32, level DetailLevel) (*ThreadResult, error) {
	threadData, err := c.UIDThread(&imapclient.ThreadOptions{
		Algorithm:      imap.ThreadReferences,
		SearchCriteria: &imap.SearchCriteria{},
	}).Wait()
	if err != nil {
		return nil, fmt.Errorf("THREAD command: %w", err)
	}

	// Find the subtree containing our target UID
	var threadUIDs []uint32
	for _, td := range threadData {
		if subtreeContains(td, uid) {
			threadUIDs = collectUIDs(td)
			break
		}
	}

	if len(threadUIDs) == 0 {
		threadUIDs = []uint32{uid}
	}

	uidSet := imap.UIDSet{}
	for _, u := range threadUIDs {
		uidSet.AddNum(imap.UID(u))
	}

	messages, err := FetchByLevel(ctx, c, uidSet, level, false)
	if err != nil {
		return nil, err
	}

	sortMessagesByDate(messages)

	return &ThreadResult{
		Messages: messages,
	}, nil
}

// getThreadViaReferences uses Message-ID/References/In-Reply-To headers to find thread members.
func getThreadViaReferences(ctx context.Context, c *imapclient.Client, folder string, uid uint32, level DetailLevel) (*ThreadResult, error) {
	// First, fetch the target message's full headers to get Message-ID, References, In-Reply-To
	targetUID := imap.UIDSet{}
	targetUID.AddNum(imap.UID(uid))

	fetchOpts := &imap.FetchOptions{
		Envelope: true,
		UID:      true,
		BodySection: []*imap.FetchItemBodySection{
			{Specifier: imap.PartSpecifierHeader, Peek: true},
		},
	}

	msgs, err := c.Fetch(targetUID, fetchOpts).Collect()
	if err != nil {
		return nil, fmt.Errorf("fetching target message headers: %w", err)
	}
	if len(msgs) == 0 {
		return nil, fmt.Errorf("message UID %d not found", uid)
	}

	msg := msgs[0]
	messageID := ""
	if msg.Envelope != nil {
		messageID = msg.Envelope.MessageID
	}

	// Parse headers for References and In-Reply-To
	var references, inReplyTo string
	for _, bs := range msg.BodySection {
		if bs.Bytes != nil {
			refs, irt := parseThreadHeaders(bs.Bytes)
			references = refs
			inReplyTo = irt
		}
	}

	// Build set of all related Message-IDs
	relatedIDs := make(map[string]bool)
	if messageID != "" {
		relatedIDs[messageID] = true
	}
	for _, id := range parseMessageIDList(references) {
		relatedIDs[id] = true
	}
	if inReplyTo != "" {
		for _, id := range parseMessageIDList(inReplyTo) {
			relatedIDs[id] = true
		}
	}

	// Search for messages referencing any of these IDs
	allUIDs := map[imap.UID]bool{imap.UID(uid): true}

	for id := range relatedIDs {
		// Search for messages with this Message-ID
		criteria := &imap.SearchCriteria{
			Header: []imap.SearchCriteriaHeaderField{
				{Key: "Message-ID", Value: id},
			},
		}
		if searchData, err := c.UIDSearch(criteria, nil).Wait(); err == nil {
			for _, u := range searchData.AllUIDs() {
				allUIDs[u] = true
			}
		}

		// Search for messages referencing this Message-ID
		criteria = &imap.SearchCriteria{
			Header: []imap.SearchCriteriaHeaderField{
				{Key: "References", Value: id},
			},
		}
		if searchData, err := c.UIDSearch(criteria, nil).Wait(); err == nil {
			for _, u := range searchData.AllUIDs() {
				allUIDs[u] = true
			}
		}

		// Cap at 50 UIDs to avoid excessive fetching
		if len(allUIDs) >= 50 {
			break
		}
	}

	uidSet := imap.UIDSet{}
	for u := range allUIDs {
		uidSet.AddNum(u)
	}

	messages, err := FetchByLevel(ctx, c, uidSet, level, false)
	if err != nil {
		return nil, err
	}

	sortMessagesByDate(messages)

	return &ThreadResult{
		Messages: messages,
	}, nil
}

// subtreeContains checks if a ThreadData tree contains the given UID.
func subtreeContains(td imapclient.ThreadData, uid uint32) bool {
	for _, u := range td.Chain {
		if u == uid {
			return true
		}
	}
	for _, sub := range td.SubThreads {
		if subtreeContains(sub, uid) {
			return true
		}
	}
	return false
}

// collectUIDs collects all UIDs from a ThreadData tree.
func collectUIDs(td imapclient.ThreadData) []uint32 {
	uids := append([]uint32{}, td.Chain...)
	for _, sub := range td.SubThreads {
		uids = append(uids, collectUIDs(sub)...)
	}
	return uids
}

// sortMessagesByDate sorts messages by date ascending.
func sortMessagesByDate(messages []MessageSummary) {
	sort.Slice(messages, func(i, j int) bool {
		return messages[i].Date < messages[j].Date
	})
}

// parseThreadHeaders extracts References and In-Reply-To from raw header bytes.
func parseThreadHeaders(headerBytes []byte) (references, inReplyTo string) {
	headers := string(headerBytes)
	for _, line := range splitHeaders(headers) {
		lower := toLowerASCII(line)
		if len(lower) > 12 && lower[:12] == "references: " {
			references = line[12:]
		} else if len(lower) > 13 && lower[:13] == "in-reply-to: " {
			inReplyTo = line[13:]
		}
	}
	return
}

// splitHeaders splits raw headers into individual header lines, handling folding.
func splitHeaders(raw string) []string {
	var headers []string
	var current string
	for _, line := range splitLines(raw) {
		if len(line) > 0 && (line[0] == ' ' || line[0] == '\t') {
			// Continuation of previous header
			current += " " + trimSpace(line)
		} else {
			if current != "" {
				headers = append(headers, current)
			}
			current = trimSpace(line)
		}
	}
	if current != "" {
		headers = append(headers, current)
	}
	return headers
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			line := s[start:i]
			if len(line) > 0 && line[len(line)-1] == '\r' {
				line = line[:len(line)-1]
			}
			lines = append(lines, line)
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

func trimSpace(s string) string {
	i := 0
	for i < len(s) && (s[i] == ' ' || s[i] == '\t') {
		i++
	}
	j := len(s)
	for j > i && (s[j-1] == ' ' || s[j-1] == '\t' || s[j-1] == '\r' || s[j-1] == '\n') {
		j--
	}
	return s[i:j]
}

func toLowerASCII(s string) string {
	b := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		b[i] = c
	}
	return string(b)
}

// parseMessageIDList parses a space/comma-separated list of Message-IDs (angle-bracket format).
func parseMessageIDList(s string) []string {
	var ids []string
	inAngle := false
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '<' {
			inAngle = true
			start = i
		} else if s[i] == '>' && inAngle {
			ids = append(ids, s[start:i+1])
			inAngle = false
		}
	}
	return ids
}
