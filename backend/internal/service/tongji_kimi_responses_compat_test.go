//go:build unit

package service

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func tongjiKimiTestAccount() *Account {
	a := forceChatResponsesFallbackAccount()
	a.Credentials["base_url"] = "https://agi.tongji.edu.cn/v1"
	a.Extra["openai_responses_mode"] = "force_responses"
	a.Extra["openai_responses_supported"] = true
	a.Extra["openai_chat_compaction_enabled"] = true
	return a
}

func TestTongjiKimiResponsesCompatibility(t *testing.T) {
	for _, compact := range []bool{false, true} {
		t.Run(map[bool]string{false: "tools", true: "compaction"}[compact], func(t *testing.T) {
			input := `[{"type":"message","role":"developer","content":"Keep ALPHA123."},{"type":"additional_tools","role":"developer","tools":[{"type":"function","name":"probe_lookup","parameters":{"type":"object","properties":{}}}]},{"type":"message","role":"user","content":"Remember ALPHA123."}`
			if compact {
				input += `,{"type":"compaction_trigger"}`
			}
			body := []byte(`{"model":"Kimi-K3.1-Pro","stream":false,"input":` + input + `]}`)
			upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"id":"cc_kimi","choices":[{"index":0,"message":{"role":"assistant","content":"Remember ALPHA123."},"finish_reason":"stop"}],"usage":{"prompt_tokens":12,"completion_tokens":5,"total_tokens":17}}`))}}
			cfg := rawChatCompletionsTestConfig()
			cfg.JWT.Secret = "kimi-compat-test-secret"
			svc := &OpenAIGatewayService{cfg: cfg, httpUpstream: upstream}
			a := tongjiKimiTestAccount()
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest("POST", "/v1/responses", bytes.NewReader(body))
			_, err := svc.Forward(context.Background(), c, a, body)
			require.NotNil(t, upstream.lastReq)
			require.Equal(t, "/v1/chat/completions", upstream.lastReq.URL.Path)
			require.NoError(t, err)
			require.Equal(t, 200, rec.Code)
			require.Equal(t, "system", gjson.GetBytes(upstream.lastBody, "messages.0.role").String())
			require.NotContains(t, string(upstream.lastBody), "additional_tools")
			require.Equal(t, "force_responses", a.Extra["openai_responses_mode"], "shared account must not be mutated")
			if compact {
				require.False(t, gjson.GetBytes(upstream.lastBody, "tools").Exists())
				require.NotContains(t, string(upstream.lastBody), "compaction_trigger")
				encrypted := gjson.Get(rec.Body.String(), "output.0.encrypted_content").String()
				summary, openErr := svc.openChatCompaction(encrypted)
				require.NoError(t, openErr)
				require.Contains(t, summary, "ALPHA123")
			} else {
				require.Equal(t, "probe_lookup", gjson.GetBytes(upstream.lastBody, "tools.0.function.name").String())
			}
		})
	}
}

func TestTongjiKimiCompatibilityScope(t *testing.T) {
	a := tongjiKimiTestAccount()
	for _, model := range []string{"DeepSeek-V4.1-Flash", "MiniMax-M3.1-Flash", "gpt-6.1-sol"} {
		require.Same(t, a, tongjiKimiChatCompatibilityAccount(a, model))
	}
	other := *a
	other.Credentials = map[string]any{"base_url": "https://api.moonshot.cn/v1"}
	require.Same(t, &other, tongjiKimiChatCompatibilityAccount(&other, "Kimi-K3.1-Pro"))
	require.Nil(t, tongjiKimiChatCompatibilityAccount(nil, "Kimi-K3.1-Pro"))
	clone := tongjiKimiChatCompatibilityAccount(a, "Kimi-K3.1-Pro")
	require.NotSame(t, a, clone)
	require.True(t, shouldForwardOpenAIResponsesViaRawChatCompletions(clone))
	require.True(t, clone.IsOpenAIChatCompactionEnabled())
	require.Equal(t, "force_responses", a.Extra["openai_responses_mode"])
}
