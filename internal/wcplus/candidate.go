package wcplus

import (
	"encoding/json"
	"strings"
)

// GzhCandidate is parsed from search or list APIs.
type GzhCandidate struct {
	Biz      string `json:"Biz"`
	Nickname string `json:"Nickname"`
	Img      string `json:"Img"`
}

// ParseSearchCandidates extracts list items from wcplus search JSON (shape varies by version).
func ParseSearchCandidates(raw json.RawMessage) ([]GzhCandidate, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	if list, err := decodeCandidateList(raw); err != nil {
		return nil, err
	} else if len(list) > 0 {
		return list, nil
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, err
	}
	for _, key := range []string{"Items", "items", "Gzhs", "gzhs", "Data", "data", "List", "list", "Result", "result"} {
		if chunk, ok := envelope[key]; ok {
			if list, err := decodeCandidateList(chunk); err != nil {
				return nil, err
			} else if len(list) > 0 {
				return list, nil
			}
		}
	}
	return parseLooseCandidates(raw)
}

func decodeCandidateList(raw json.RawMessage) ([]GzhCandidate, error) {
	var list []GzhCandidate
	if err := json.Unmarshal(raw, &list); err == nil && len(list) > 0 {
		return normalizeCandidates(list), nil
	}
	var wraps []struct {
		Items []GzhCandidate `json:"Items"`
	}
	if err := json.Unmarshal(raw, &wraps); err == nil {
		for _, w := range wraps {
			if len(w.Items) > 0 {
				return normalizeCandidates(w.Items), nil
			}
		}
	}
	var wrap struct {
		Items []GzhCandidate `json:"Items"`
		Gzhs  []GzhCandidate `json:"Gzhs"`
	}
	if err := json.Unmarshal(raw, &wrap); err == nil {
		if len(wrap.Items) > 0 {
			return normalizeCandidates(wrap.Items), nil
		}
		if len(wrap.Gzhs) > 0 {
			return normalizeCandidates(wrap.Gzhs), nil
		}
	}
	return nil, nil
}

func parseLooseCandidates(raw json.RawMessage) ([]GzhCandidate, error) {
	var rows []map[string]any
	if err := json.Unmarshal(raw, &rows); err != nil {
		return nil, nil
	}
	var out []GzhCandidate
	for _, row := range rows {
		c := mapToCandidate(row)
		if c.Biz != "" || c.Nickname != "" {
			out = append(out, c)
		}
	}
	return out, nil
}

func mapToCandidate(row map[string]any) GzhCandidate {
	return GzhCandidate{
		Biz:      pickString(row, "Biz", "biz", "BIZ"),
		Nickname: pickString(row, "Nickname", "nickname", "NickName", "name"),
		Img:      pickString(row, "Img", "img", "Avatar", "avatar"),
	}
}

func pickString(row map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := row[k]; ok {
			if s, ok := v.(string); ok {
				return strings.TrimSpace(s)
			}
		}
	}
	return ""
}

func normalizeCandidates(in []GzhCandidate) []GzhCandidate {
	out := make([]GzhCandidate, 0, len(in))
	for _, c := range in {
		c.Biz = strings.TrimSpace(c.Biz)
		c.Nickname = strings.TrimSpace(c.Nickname)
		c.Img = strings.TrimSpace(c.Img)
		if c.Biz != "" || c.Nickname != "" {
			out = append(out, c)
		}
	}
	return out
}

// MatchNicknameExact finds candidate with exact nickname (wcplus batch import rule).
func MatchNicknameExact(candidates []GzhCandidate, nickname string) (GzhCandidate, bool) {
	want := strings.TrimSpace(nickname)
	for _, c := range candidates {
		if strings.TrimSpace(c.Nickname) == want {
			return c, true
		}
	}
	return GzhCandidate{}, false
}

// MatchBizExact returns the candidate whose Biz exactly matches the requested account.
func MatchBizExact(candidates []GzhCandidate, biz string) (GzhCandidate, bool) {
	want := strings.TrimSpace(biz)
	for _, c := range candidates {
		if strings.TrimSpace(c.Biz) == want {
			return c, true
		}
	}
	return GzhCandidate{}, false
}

// GzhSummaryToCandidate converts list row.
func GzhSummaryToCandidate(g GzhSummary) GzhCandidate {
	return GzhCandidate{Biz: g.Biz, Nickname: g.Nickname, Img: g.Img}
}
