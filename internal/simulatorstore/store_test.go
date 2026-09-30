package simulatorstore

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestStoreLatestAndMissingAccount(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "store.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertAccount("biz-1", "测试号", ""); err != nil {
		t.Fatal(err)
	}
	if err := store.AddArticle("biz-1", "测试号", "https://example/old", "旧", "", "", 10); err != nil {
		t.Fatal(err)
	}
	if err := store.AddArticle("biz-1", "测试号", "https://example/new", "新", "", "", 20); err != nil {
		t.Fatal(err)
	}
	account, articles, err := store.Latest("biz-1", 1)
	if err != nil || account.Nickname != "测试号" || len(articles) != 1 || articles[0].Link != "https://example/new" || articles[0].Content != "" {
		t.Fatalf("unexpected latest result: account=%#v articles=%#v err=%v", account, articles, err)
	}
	_, _, err = store.Latest("missing", 10)
	if !errors.Is(err, ErrAccountNotFound) {
		t.Fatalf("expected ErrAccountNotFound, got %v", err)
	}
}

func TestAddArticleFromKeepsRicherFields(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "store.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertAccount("biz-1", "测试号", "avatar"); err != nil {
		t.Fatal(err)
	}
	if err := store.AddArticleFrom("wcplus", "biz-1", "测试号", "https://example/a", "标题", "cover", "<p>正文</p>", 100); err != nil {
		t.Fatal(err)
	}
	if err := store.AddArticleFrom("simulator", "biz-1", "测试号", "https://example/a", "", "", "", 0); err != nil {
		t.Fatal(err)
	}
	_, articles, err := store.Latest("biz-1", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(articles) != 1 {
		t.Fatalf("articles=%d", len(articles))
	}
	got := articles[0]
	if got.Title != "标题" || got.Image != "cover" || got.Content != "<p>正文</p>" || got.PublishedAt != 100 || got.Source != "simulator" {
		t.Fatalf("richer fields were overwritten: %#v", got)
	}
}

func TestSQLiteStorePersistsAndKeepsRicherFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "store.sqlite3")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertAccount("biz-sqlite", "SQLite号", "avatar"); err != nil {
		t.Fatal(err)
	}
	if err := store.AddArticleFrom("external", "biz-sqlite", "SQLite号", "https://example/sqlite", "标题", "cover", "<p>正文</p>", 100); err != nil {
		t.Fatal(err)
	}
	if err := store.AddArticleFrom("sync", "biz-sqlite", "SQLite号", "https://example/sqlite", "", "", "", 0); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	account, articles, err := reopened.Latest("biz-sqlite", 10)
	if err != nil || account.Nickname != "SQLite号" || len(articles) != 1 {
		t.Fatalf("unexpected sqlite result: account=%#v articles=%#v err=%v", account, articles, err)
	}
	got := articles[0]
	if got.Title != "标题" || got.Image != "cover" || got.Content != "<p>正文</p>" || got.PublishedAt != 100 || got.Source != "sync" {
		t.Fatalf("sqlite richer fields were overwritten: %#v", got)
	}
}

func TestSQLiteStoreMigratesLegacyJSON(t *testing.T) {
	dir := t.TempDir()
	legacy := filepath.Join(dir, "simulator-store.json")
	store, err := Open(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertAccount("biz-migrate", "迁移号", ""); err != nil {
		t.Fatal(err)
	}
	if err := store.AddArticle("biz-migrate", "迁移号", "https://example/migrate", "迁移文章", "", "正文", 200); err != nil {
		t.Fatal(err)
	}
	sqlitePath := filepath.Join(dir, "simulator-store.sqlite3")
	sqliteStore, err := Open(sqlitePath)
	if err != nil {
		t.Fatal(err)
	}
	defer sqliteStore.Close()
	_, articles, err := sqliteStore.Latest("biz-migrate", 10)
	if err != nil || len(articles) != 1 || articles[0].Title != "迁移文章" {
		t.Fatalf("legacy migration failed: articles=%#v err=%v", articles, err)
	}
}
