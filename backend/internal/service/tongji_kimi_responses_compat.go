package service

import (
	"net/url"
	"strings"
)

// Tongji's Kimi Responses endpoint rejects Codex developer/additional_tools
// items and does not implement native compaction. Reuse the existing Chat
// Completions bridge only for this model and upstream, including summary
// compaction. The database/cache account is shared across concurrent models.
func isTongjiKimiChatCompatibilityRequest(account *Account, model string) bool {
	if account == nil || !account.IsOpenAIApiKey() || !strings.EqualFold(strings.TrimSpace(model), "Kimi-K3.1-Pro") {
		return false
	}
	u, err := url.Parse(account.GetOpenAIBaseURL())
	return err == nil && strings.EqualFold(u.Hostname(), "agi.tongji.edu.cn")
}

func tongjiKimiChatCompatibilityAccount(account *Account, model string) *Account {
	if !isTongjiKimiChatCompatibilityRequest(account, model) {
		return account
	}
	clone := *account
	clone.Extra = make(map[string]any, len(account.Extra)+2)
	for k, v := range account.Extra {
		clone.Extra[k] = v
	}
	clone.Extra["openai_responses_mode"] = "force_chat_completions"
	clone.Extra["openai_chat_compaction_enabled"] = true
	return &clone
}
