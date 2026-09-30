package wcplus

import (
	"encoding/json"
	"strings"
)

// UnmarshalJSON accepts wcplus article list rows (field names vary by version).
func (a *ArticleSummary) UnmarshalJSON(b []byte) error {
	type plain ArticleSummary
	if err := json.Unmarshal(b, (*plain)(a)); err != nil {
		return err
	}
	var row map[string]any
	if err := json.Unmarshal(b, &row); err != nil {
		return nil
	}
	if a.CoverURL == "" {
		a.CoverURL = pickString(row,
			"CoverURL", "CoverUrl", "cover_url", "Cover", "cover",
			"Img", "img", "Image", "image",
			"ThumbUrl", "ThumbURL", "thumb_url", "Thumb", "thumb",
			"CdnUrl", "cdn_url", "PicUrl", "pic_url",
		)
	}
	if a.Title == "" {
		a.Title = pickString(row, "Title", "title", "Name", "name")
	}
	if a.Link == "" {
		a.Link = pickString(row, "Link", "link", "Url", "url")
	}
	return nil
}

// BestLink prefers permanent article URL for ADP.
func (a ArticleSummary) BestLink() string {
	for _, u := range []string{a.Link, a.ContentURL, a.SourceURL} {
		if s := strings.TrimSpace(u); s != "" {
			return s
		}
	}
	return ""
}

// CoverImage returns list-row cover if present.
func (a ArticleSummary) CoverImage() string {
	return strings.TrimSpace(a.CoverURL)
}
