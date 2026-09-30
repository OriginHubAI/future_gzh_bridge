package service

import "fmt"

// OfficialAccountNotFoundError is returned when biz is not imported in wcplus (ADP contract).
type OfficialAccountNotFoundError struct {
	Biz      string
	Nickname string
}

func (e *OfficialAccountNotFoundError) Error() string {
	if e.Biz != "" {
		return fmt.Sprintf("official account not found in wcplus (biz=%s)", e.Biz)
	}
	return "official account not found in wcplus"
}

// OfficialAccountExistsError is returned when the account is already in wcplus (ADP contract).
type OfficialAccountExistsError struct {
	Biz      string
	Nickname string
}

func (e *OfficialAccountExistsError) Error() string {
	return fmt.Sprintf("official account already imported (biz=%s)", e.Biz)
}

// CollectorNotReadyError means wcplus/Max/wechat params are not ready for import/export.
type CollectorNotReadyError struct {
	Message string
}

func (e *CollectorNotReadyError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return "collector not ready"
}
