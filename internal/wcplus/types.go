package wcplus

import "encoding/json"

// GzhListResponse mirrors wcplusPro /api/gzh/list (fields may vary by version).
type GzhListResponse struct {
	Gzhs     []GzhSummary `json:"Gzhs"`
	Total    int          `json:"Total"`
	Articles int          `json:"Articles"`
	Raw      json.RawMessage `json:"-"`
}

type GzhSummary struct {
	Biz      string `json:"Biz"`
	Nickname string `json:"Nickname"`
	Img      string `json:"Img"`
	Articles int    `json:"Articles"`
}

type GzhArticlesResponse struct {
	Articles []ArticleSummary `json:"Articles"`
	Total    int              `json:"Total"`
	Gzh      *GzhSummary      `json:"Gzh"`
}

type ArticleSummary struct {
	ID         string `json:"ID"`
	Title      string `json:"Title"`
	Link       string `json:"Link"`
	ContentURL string `json:"ContentURL"`
	SourceURL  string `json:"SourceURL"`
	CoverURL   string `json:"CoverURL,omitempty"`
	PDate      int64  `json:"PDate"` // wcplus: Unix 秒
	ReadNum    int    `json:"ReadNum"`
	LikeNum    int    `json:"LikeNum"`
	CommentNum int    `json:"CommentNum"`
	Mov        int    `json:"Mov"`
	Author     string `json:"Author"`
	Nickname   string `json:"Nickname"`
}

type ArticleContentResponse struct {
	Content  string `json:"Content"`
	Title    string `json:"Title"`
	Nickname string `json:"Nickname"`
	ID       string `json:"ID"`
}
