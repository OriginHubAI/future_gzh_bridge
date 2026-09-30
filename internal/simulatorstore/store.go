package simulatorstore

import (
	"crypto/sha1"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

var ErrAccountNotFound = errors.New("公众号不存在")

type Account struct {
	Biz       string    `json:"biz"`
	Nickname  string    `json:"nickname"`
	Avatar    string    `json:"avatar,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type Article struct {
	ID          string    `json:"id"`
	Biz         string    `json:"biz"`
	Title       string    `json:"title"`
	PublishedAt int64     `json:"publishedAt"`
	Link        string    `json:"link"`
	Image       string    `json:"image"`
	Content     string    `json:"content"`
	Source      string    `json:"source,omitempty"`
	FetchedAt   time.Time `json:"fetchedAt"`
}

type state struct {
	Accounts []Account `json:"accounts"`
	Articles []Article `json:"articles"`
}

// Store supports the original JSON format and SQLite. The format is selected
// by the configured file extension: .json uses JSON, while .db/.sqlite/.sqlite3
// use SQLite. The public store API is shared by both implementations.
type Store struct {
	mu    sync.RWMutex
	path  string
	state state
	db    *sql.DB
}

func Open(path string) (*Store, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, errors.New("simulator store path is empty")
	}
	if isSQLitePath(path) {
		return openSQLite(path)
	}
	return openJSON(path)
}

func isSQLitePath(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".db", ".sqlite", ".sqlite3":
		return true
	default:
		return false
	}
}

func openJSON(path string) (*Store, error) {
	s := &Store{path: path, state: state{Accounts: []Account{}, Articles: []Article{}}}
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if len(raw) > 0 && json.Unmarshal(raw, &s.state) != nil {
		return nil, errors.New("decode simulator store failed")
	}
	return s, nil
}

func openSQLite(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err = db.Exec(`PRAGMA busy_timeout = 5000`); err != nil {
		db.Close()
		return nil, err
	}
	if _, err = db.Exec(`PRAGMA foreign_keys = ON`); err != nil {
		db.Close()
		return nil, err
	}
	if _, err = db.Exec(`
CREATE TABLE IF NOT EXISTS official_accounts (
	biz TEXT PRIMARY KEY,
	nickname TEXT NOT NULL DEFAULT '',
	avatar TEXT NOT NULL DEFAULT '',
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS articles (
	id TEXT PRIMARY KEY,
	biz TEXT NOT NULL,
	title TEXT NOT NULL DEFAULT '',
	published_at INTEGER NOT NULL DEFAULT 0,
	link TEXT NOT NULL,
	image TEXT NOT NULL DEFAULT '',
	content TEXT NOT NULL DEFAULT '',
	source TEXT NOT NULL DEFAULT '',
	fetched_at TEXT NOT NULL,
	FOREIGN KEY (biz) REFERENCES official_accounts(biz) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_articles_biz_published ON articles (biz, published_at DESC);
`); err != nil {
		db.Close()
		return nil, err
	}
	if err := migrateLegacyJSON(db, path); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{path: path, db: db}, nil
}

func migrateLegacyJSON(db *sql.DB, sqlitePath string) error {
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM official_accounts`).Scan(&count); err != nil || count > 0 {
		return err
	}
	legacyPath := strings.TrimSuffix(sqlitePath, filepath.Ext(sqlitePath)) + ".json"
	raw, err := os.ReadFile(legacyPath)
	if errors.Is(err, os.ErrNotExist) || len(raw) == 0 {
		return nil
	}
	if err != nil {
		return err
	}
	var old state
	if err := json.Unmarshal(raw, &old); err != nil {
		return errors.New("decode legacy simulator store failed")
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, account := range old.Accounts {
		if _, err := tx.Exec(`INSERT OR IGNORE INTO official_accounts (biz,nickname,avatar,created_at,updated_at) VALUES (?,?,?,?,?)`, account.Biz, account.Nickname, account.Avatar, account.CreatedAt.Format(time.RFC3339Nano), account.UpdatedAt.Format(time.RFC3339Nano)); err != nil {
			return err
		}
	}
	for _, article := range old.Articles {
		if _, err := tx.Exec(`INSERT OR IGNORE INTO articles (id,biz,title,published_at,link,image,content,source,fetched_at) VALUES (?,?,?,?,?,?,?,?,?)`, article.ID, article.Biz, article.Title, article.PublishedAt, article.Link, article.Image, article.Content, article.Source, article.FetchedAt.Format(time.RFC3339Nano)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}

func (s *Store) UpsertAccount(biz, nickname, avatar string) error {
	biz = strings.TrimSpace(biz)
	if biz == "" {
		return errors.New("biz is required")
	}
	nickname = strings.TrimSpace(nickname)
	avatar = strings.TrimSpace(avatar)
	now := time.Now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db != nil {
		_, err := s.db.Exec(`INSERT INTO official_accounts (biz,nickname,avatar,created_at,updated_at)
VALUES (?,?,?,?,?) ON CONFLICT(biz) DO UPDATE SET
nickname=CASE WHEN excluded.nickname <> '' THEN excluded.nickname ELSE official_accounts.nickname END,
avatar=CASE WHEN excluded.avatar <> '' THEN excluded.avatar ELSE official_accounts.avatar END,
updated_at=excluded.updated_at`, biz, nickname, avatar, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
		return err
	}
	for i := range s.state.Accounts {
		if s.state.Accounts[i].Biz == biz {
			if nickname != "" {
				s.state.Accounts[i].Nickname = nickname
			}
			if avatar != "" {
				s.state.Accounts[i].Avatar = avatar
			}
			s.state.Accounts[i].UpdatedAt = now
			return s.persistLocked()
		}
	}
	s.state.Accounts = append(s.state.Accounts, Account{Biz: biz, Nickname: nickname, Avatar: avatar, CreatedAt: now, UpdatedAt: now})
	return s.persistLocked()
}

func (s *Store) FindAccount(biz, nickname string) (Account, bool) {
	biz = strings.TrimSpace(biz)
	nickname = strings.TrimSpace(nickname)
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.db != nil {
		var account Account
		var createdAt, updatedAt string
		var row *sql.Row
		if biz != "" {
			row = s.db.QueryRow(`SELECT biz,nickname,avatar,created_at,updated_at FROM official_accounts WHERE biz=?`, biz)
		} else if nickname != "" {
			row = s.db.QueryRow(`SELECT biz,nickname,avatar,created_at,updated_at FROM official_accounts WHERE nickname=?`, nickname)
		} else {
			return Account{}, false
		}
		if err := row.Scan(&account.Biz, &account.Nickname, &account.Avatar, &createdAt, &updatedAt); err != nil {
			return Account{}, false
		}
		account.CreatedAt = parseTime(createdAt)
		account.UpdatedAt = parseTime(updatedAt)
		return account, true
	}
	for _, account := range s.state.Accounts {
		if biz != "" && account.Biz == biz {
			return account, true
		}
	}
	if nickname == "" {
		return Account{}, false
	}
	for _, account := range s.state.Accounts {
		if account.Nickname == nickname {
			return account, true
		}
	}
	return Account{}, false
}

func (s *Store) AddArticle(biz, nickname, link, title, image, content string, publishedAt int64) error {
	return s.AddArticleFrom("", biz, nickname, link, title, image, content, publishedAt)
}

// AddArticleFrom stores one article from a crawler engine. biz+link is the
// identity. A later engine that only has a link does not wipe richer fields.
func (s *Store) AddArticleFrom(source, biz, nickname, link, title, image, content string, publishedAt int64) error {
	biz = strings.TrimSpace(biz)
	link = strings.TrimSpace(link)
	title = strings.TrimSpace(title)
	image = strings.TrimSpace(image)
	content = strings.TrimSpace(content)
	source = strings.TrimSpace(source)
	if biz == "" || link == "" {
		return errors.New("biz and link are required")
	}
	idBytes := sha1.Sum([]byte(biz + "\x00" + link))
	id := hex.EncodeToString(idBytes[:])
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db != nil {
		return s.addArticleSQLiteLocked(id, source, biz, link, title, image, content, publishedAt)
	}
	for i := range s.state.Articles {
		if s.state.Articles[i].ID != id {
			continue
		}
		if title != "" {
			s.state.Articles[i].Title = title
		}
		if image != "" {
			s.state.Articles[i].Image = image
		}
		if content != "" {
			s.state.Articles[i].Content = content
		}
		if publishedAt > 0 {
			s.state.Articles[i].PublishedAt = publishedAt
		}
		if source != "" {
			s.state.Articles[i].Source = source
		}
		s.state.Articles[i].FetchedAt = time.Now().UTC()
		_ = nickname
		return s.persistLocked()
	}
	if publishedAt <= 0 {
		publishedAt = time.Now().Unix()
	}
	s.state.Articles = append(s.state.Articles, Article{
		ID: id, Biz: biz, Title: title, PublishedAt: publishedAt, Link: link,
		Image: image, Content: content, Source: source, FetchedAt: time.Now().UTC(),
	})
	_ = nickname
	return s.persistLocked()
}

func (s *Store) addArticleSQLiteLocked(id, source, biz, link, title, image, content string, publishedAt int64) error {
	if publishedAt <= 0 {
		publishedAt = time.Now().Unix()
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := s.db.Exec(`INSERT INTO articles (id,biz,title,published_at,link,image,content,source,fetched_at)
VALUES (?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET
title=CASE WHEN excluded.title <> '' THEN excluded.title ELSE articles.title END,
published_at=CASE WHEN excluded.published_at > 0 THEN excluded.published_at ELSE articles.published_at END,
image=CASE WHEN excluded.image <> '' THEN excluded.image ELSE articles.image END,
content=CASE WHEN excluded.content <> '' THEN excluded.content ELSE articles.content END,
source=CASE WHEN excluded.source <> '' THEN excluded.source ELSE articles.source END,
fetched_at=excluded.fetched_at`, id, biz, title, publishedAt, link, image, content, source, now)
	return err
}

func (s *Store) Latest(biz string, limit int) (Account, []Article, error) {
	biz = strings.TrimSpace(biz)
	if limit <= 0 {
		limit = 10
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.db != nil {
		account, err := s.accountSQLiteLocked(biz)
		if err != nil {
			return Account{}, nil, err
		}
		rows, err := s.db.Query(`SELECT id,biz,title,published_at,link,image,content,source,fetched_at
FROM articles WHERE biz=? ORDER BY published_at DESC LIMIT ?`, biz, limit)
		if err != nil {
			return Account{}, nil, err
		}
		defer rows.Close()
		articles := make([]Article, 0)
		for rows.Next() {
			var article Article
			var fetchedAt string
			if err := rows.Scan(&article.ID, &article.Biz, &article.Title, &article.PublishedAt, &article.Link, &article.Image, &article.Content, &article.Source, &fetchedAt); err != nil {
				return Account{}, nil, err
			}
			article.FetchedAt = parseTime(fetchedAt)
			articles = append(articles, article)
		}
		return account, articles, rows.Err()
	}
	var account Account
	found := false
	for _, a := range s.state.Accounts {
		if a.Biz == biz {
			account, found = a, true
			break
		}
	}
	if !found {
		return Account{}, nil, ErrAccountNotFound
	}
	articles := make([]Article, 0)
	for _, a := range s.state.Articles {
		if a.Biz == biz {
			articles = append(articles, a)
		}
	}
	sort.SliceStable(articles, func(i, j int) bool { return articles[i].PublishedAt > articles[j].PublishedAt })
	if limit < len(articles) {
		articles = articles[:limit]
	}
	return account, articles, nil
}

func (s *Store) accountSQLiteLocked(biz string) (Account, error) {
	var account Account
	var createdAt, updatedAt string
	err := s.db.QueryRow(`SELECT biz,nickname,avatar,created_at,updated_at FROM official_accounts WHERE biz=?`, biz).
		Scan(&account.Biz, &account.Nickname, &account.Avatar, &createdAt, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Account{}, ErrAccountNotFound
	}
	if err != nil {
		return Account{}, err
	}
	account.CreatedAt = parseTime(createdAt)
	account.UpdatedAt = parseTime(updatedAt)
	return account, nil
}

func (s *Store) persistLocked() error {
	if s.db != nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(s.state, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func (s *Store) DeleteAccount(biz string) error {
	biz = strings.TrimSpace(biz)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db != nil {
		var exists int
		if err := s.db.QueryRow(`SELECT 1 FROM official_accounts WHERE biz=?`, biz).Scan(&exists); errors.Is(err, sql.ErrNoRows) {
			return ErrAccountNotFound
		} else if err != nil {
			return err
		}
		if _, err := s.db.Exec(`DELETE FROM articles WHERE biz=?`, biz); err != nil {
			return err
		}
		_, err := s.db.Exec(`DELETE FROM official_accounts WHERE biz=?`, biz)
		return err
	}
	index := -1
	for i, a := range s.state.Accounts {
		if a.Biz == biz {
			index = i
			break
		}
	}
	if index < 0 {
		return ErrAccountNotFound
	}
	s.state.Accounts = append(s.state.Accounts[:index], s.state.Accounts[index+1:]...)
	rows := make([]Article, 0)
	for _, a := range s.state.Articles {
		if a.Biz != biz {
			rows = append(rows, a)
		}
	}
	s.state.Articles = rows
	return s.persistLocked()
}

func parseTime(raw string) time.Time {
	t, _ := time.Parse(time.RFC3339Nano, raw)
	return t
}
