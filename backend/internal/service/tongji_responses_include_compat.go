package service

import (
	"encoding/json"
	"net/url"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// Tongji validates Responses include against an older enum. Only drop the
// unsupported optional output encryption selector; reasoning encryption and
// existing encrypted input items must still round-trip unchanged.
func normalizeTongjiResponsesInclude(account *Account, body []byte) ([]byte, error) {
	if account == nil || !account.IsOpenAIApiKey() {
		return body, nil
	}
	u, err := url.Parse(account.GetOpenAIBaseURL())
	if err != nil || !strings.EqualFold(u.Hostname(), "agi.tongji.edu.cn") {
		return body, nil
	}
	include := gjson.GetBytes(body, "include")
	if !include.IsArray() {
		return body, nil
	}
	filtered := make([]json.RawMessage, 0, len(include.Array()))
	changed := false
	for _, item := range include.Array() {
		if item.Type == gjson.String && item.String() == "output.encrypted_content" {
			changed = true
			continue
		}
		filtered = append(filtered, json.RawMessage(item.Raw))
	}
	if !changed {
		return body, nil
	}
	return sjson.SetBytes(body, "include", filtered)
}
