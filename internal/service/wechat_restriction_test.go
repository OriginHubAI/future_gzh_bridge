package service

import "testing"

func TestDetectWechatContentRestriction(t *testing.T) {
	articles := []ArticleExportItem{
		{ID: "1", Link: "https://mp.weixin.qq.com/s/x", Name: "t", Content: ""},
	}
	if !detectWechatContentRestriction(true, articles, nil) {
		t.Fatal("expected restricted when all content empty")
	}
	articles[0].Content = "<p>hi</p>"
	if detectWechatContentRestriction(true, articles, nil) {
		t.Fatal("expected not restricted when content present")
	}
	if detectWechatContentRestriction(false, articles, nil) {
		t.Fatal("withContent false")
	}
	if !detectWechatContentRestriction(true, articles, []error{errFake("采集受限")}) {
		t.Fatal("expected block keyword in wcplus error")
	}
}

type errFake string

func (e errFake) Error() string { return string(e) }
