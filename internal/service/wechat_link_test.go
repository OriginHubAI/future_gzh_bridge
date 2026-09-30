package service

import "testing"

func TestBizFromWechatURL(t *testing.T) {
	raw := "https://mp.weixin.qq.com/mp/profile_ext?action=home&__biz=MzA5MDM2MTI3MA==&scene=124"
	biz, ok := BizFromWechatURL(raw)
	if !ok || biz != "MzA5MDM2MTI3MA==" {
		t.Fatalf("got %q ok=%v", biz, ok)
	}
}
