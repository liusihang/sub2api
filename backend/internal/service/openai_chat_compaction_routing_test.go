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

func TestGatewayChatCompactionWithNativeResponsesAccounts(t *testing.T) {
	for _, model := range []string{"DeepSeek-V4.1-Flash", "MiniMax-M3.1-Flash", "future-vendor-new-model"} {
		for _, transport := range []string{"http", "legacy", "ws"} {
			t.Run(model+"/"+transport, func(t *testing.T) {
				account := tongjiKimiTestAccount()
				account.Credentials["base_url"] = "https://relay.example.test/v1"
				input := `[{"role":"developer","content":"Keep TOKEN731."},{"role":"user","content":"Remember TOKEN731."},{"type":"additional_tools","tools":[{"type":"function","name":"read","parameters":{"type":"object"}}]}`
				if transport != "legacy" {
					input += `,{"type":"compaction_trigger"}`
				}
				input += `]`
				body := []byte(`{"model":"` + model + `","stream":false,"input":` + input + `}`)
				upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"id":"cc_summary","choices":[{"message":{"role":"assistant","content":"Remember TOKEN731."},"finish_reason":"stop"}],"usage":{"prompt_tokens":12,"completion_tokens":5}}`))}}
				cfg := rawChatCompletionsTestConfig()
				cfg.JWT.Secret = "gateway-native-compaction-test"
				svc := &OpenAIGatewayService{cfg: cfg, httpUpstream: upstream}
				recorder := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(recorder)
				path := "/v1/responses"
				if transport == "legacy" {
					path += "/compact"
				}
				c.Request = httptest.NewRequest("POST", path, bytes.NewReader(body))
				var err error
				if transport == "ws" {
					var frames [][]byte
					_, err = svc.proxyOpenAIWSHTTPBridgeTurn(context.Background(), c, account, "test-token", body, len(body), model, model, "", "", "", 1, func(p []byte) error { frames = append(frames, bytes.Clone(p)); return nil })
					require.NoError(t, err)
					require.NotEmpty(t, frames)
					require.Contains(t, string(bytes.Join(frames, []byte("\n"))), `"type":"compaction"`)
				} else {
					_, err = svc.Forward(context.Background(), c, account, body)
					require.NoError(t, err)
					require.Equal(t, "compaction", gjson.Get(recorder.Body.String(), "output.0.type").String())
					encrypted := gjson.Get(recorder.Body.String(), "output.0.encrypted_content").String()
					summary, openErr := svc.openChatCompaction(encrypted)
					require.NoError(t, openErr)
					require.Contains(t, summary, "TOKEN731")
				}
				require.NotNil(t, upstream.lastReq)
				require.Equal(t, "/v1/chat/completions", upstream.lastReq.URL.Path)
				require.NotContains(t, string(upstream.lastBody), "compaction_trigger")
				require.NotContains(t, string(upstream.lastBody), "additional_tools")
				require.False(t, gjson.GetBytes(upstream.lastBody, "tools").Exists())
				require.False(t, gjson.GetBytes(upstream.lastBody, "stream").Bool())
				require.Equal(t, "force_responses", account.Extra["openai_responses_mode"], "request routing must not mutate a shared account")
			})
		}
	}
}

func TestGatewayChatCompactionRequestScope(t *testing.T) {
	account := tongjiKimiTestAccount()
	normal := []byte(`{"model":"a-new-model","input":[{"role":"user","content":"hello"}]}`)
	compact := []byte(`{"model":"a-new-model","input":[{"type":"compaction_trigger"}]}`)
	require.True(t, account.IsOpenAIChatCompactionEnabled())
	require.False(t, shouldForwardOpenAIResponsesViaRawChatCompletions(account))
	require.False(t, shouldUseGatewayChatCompaction(account, normal, false))
	require.True(t, shouldUseGatewayChatCompaction(account, compact, false))
	require.True(t, shouldUseGatewayChatCompaction(account, normal, true))
	account.Extra["openai_chat_compaction_enabled"] = false
	require.False(t, shouldUseGatewayChatCompaction(account, compact, false))
	account.Extra["openai_chat_compaction_enabled"] = true
	account.Type = AccountTypeOAuth
	require.False(t, shouldUseGatewayChatCompaction(account, compact, false))
	require.False(t, shouldUseGatewayChatCompaction(nil, compact, false))
}

func TestGatewayChatCompactionOptInPreservesNormalResponses(t *testing.T) {
	account := tongjiKimiTestAccount()
	body := []byte(`{"model":"DeepSeek-V4.1-Flash","stream":false,"input":"Reply OK."}`)
	upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"id":"resp_native","object":"response","status":"completed","model":"DeepSeek-V4.1-Flash","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"OK"}]}],"usage":{"input_tokens":1,"output_tokens":1}}`))}}
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest("POST", "/v1/responses", bytes.NewReader(body))
	_, err := svc.Forward(context.Background(), c, account, body)
	require.NoError(t, err)
	require.Equal(t, "/v1/responses", upstream.lastReq.URL.Path)
	require.True(t, gjson.GetBytes(upstream.lastBody, "input").Exists())
	require.False(t, gjson.GetBytes(upstream.lastBody, "messages").Exists())
	require.Equal(t, "OK", gjson.Get(recorder.Body.String(), "output.0.content.0.text").String())
}
