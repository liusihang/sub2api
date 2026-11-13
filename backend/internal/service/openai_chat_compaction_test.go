//go:build unit

package service

import (
	"bytes"
	"context"
	coderws "github.com/coder/websocket"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestGeneralOAIChatCompactionRoundTrip(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := []byte(`{"model":"DeepSeek-V4.1-Flash","input":[{"role":"user","content":"Remember RECALL_17"},{"type":"compaction_trigger"}],"tools":[{"type":"function","name":"read","parameters":{"type":"object"}}],"stream":true}`)
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: 200,
		Header:     http.Header{"Content-Type": {"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"id":"chatcmpl_summary","choices":[{"index":0,"message":{"role":"assistant","content":"The user asked to remember RECALL_17."},"finish_reason":"stop"}],"usage":{"prompt_tokens":40,"completion_tokens":12,"total_tokens":52}}`)),
	}}
	cfg := rawChatCompletionsTestConfig()
	cfg.JWT.Secret = "general-oai-test-secret"
	svc := &OpenAIGatewayService{cfg: cfg, httpUpstream: upstream}
	account := forceChatResponsesFallbackAccount()
	account.Extra["openai_chat_compaction_enabled"] = true
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest("POST", "/v1/responses", bytes.NewReader(body))
	result, err := svc.Forward(context.Background(), c, account, body)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "/v1/chat/completions", upstream.lastReq.URL.Path)
	require.False(t, gjson.GetBytes(upstream.lastBody, "stream").Bool())
	require.False(t, gjson.GetBytes(upstream.lastBody, "tools").Exists())
	require.Contains(t, string(upstream.lastBody), "RECALL_17")
	require.NotContains(t, string(upstream.lastBody), "compaction_trigger")
	var encrypted string
	for _, line := range strings.Split(rec.Body.String(), "\n") {
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		event := strings.TrimPrefix(line, "data: ")
		if gjson.Get(event, "type").String() == "response.output_item.done" {
			require.Equal(t, "compaction", gjson.Get(event, "item.type").String())
			encrypted = gjson.Get(event, "item.encrypted_content").String()
		}
	}
	require.NotEmpty(t, encrypted)
	require.NotContains(t, encrypted, "RECALL_17")
	require.Contains(t, rec.Body.String(), "response.completed")
	require.Equal(t, 40, result.Usage.InputTokens)
	require.Equal(t, 12, result.Usage.OutputTokens)
}

func TestGeneralOAIChatCompactionCipherAndNativeIsolation(t *testing.T) {
	cfg := rawChatCompletionsTestConfig()
	cfg.JWT.Secret = "summary-secret"
	svc := &OpenAIGatewayService{cfg: cfg}
	encrypted, err := svc.sealChatCompaction("Remember RECALL_17 and /work/result.py")
	require.NoError(t, err)
	body := []byte(`{"model":"gpt-6.1-sol","input":[{"type":"compaction","encrypted_content":"` + encrypted + `"},{"role":"user","content":"continue"}]}`)
	expanded, err := svc.expandChatCompactionInputs(body)
	require.NoError(t, err)
	require.Equal(t, "user", gjson.GetBytes(expanded, "input.0.role").String())
	require.Contains(t, gjson.GetBytes(expanded, "input.0.content.0.text").String(), "RECALL_17")
	require.Equal(t, "continue", gjson.GetBytes(expanded, "input.1.content").String())
	native := []byte(`{"model":"gpt-6.1-sol","input":[{"type":"compaction","encrypted_content":"native-openai-content"}]}`)
	unchanged, err := svc.expandChatCompactionInputs(native)
	require.NoError(t, err)
	require.Equal(t, native, unchanged)
	cfg.JWT.Secret = "another-secret"
	_, err = svc.expandChatCompactionInputs(body)
	require.Error(t, err)
}

func TestGeneralOAICompactionCapabilityDoesNotEnableImages(t *testing.T) {
	account := forceChatResponsesFallbackAccount()
	require.False(t, account.SupportsOpenAIEndpointCapability(OpenAIEndpointCapabilityCompaction))
	account.Extra["openai_chat_compaction_enabled"] = true
	require.True(t, account.SupportsOpenAIEndpointCapability(OpenAIEndpointCapabilityCompaction))
	require.False(t, account.SupportsOpenAIEndpointCapability(OpenAIEndpointCapabilityResponses))
}

func TestGeneralOAIChatBridgeChunkedEvents(t *testing.T) {
	var frames []string
	w := &chatBridgeWriter{header: make(http.Header), status: 200, writeEvent: func(p []byte) error { frames = append(frames, string(p)); return nil }}
	for _, piece := range []string{"data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_1\"}}\n", "\ndata: {\"type\":\"response.output_item.done\",\"item\":{\"type\":\"function_call\",\"call_id\":\"call_1\"}}\n\ndata: [DONE]\n\n"} {
		_, err := w.Write([]byte(piece))
		require.NoError(t, err)
	}
	require.Len(t, frames, 2)
	require.Equal(t, "resp_1", w.responseID)
	require.Len(t, w.output, 1)
	require.Equal(t, "call_1", gjson.GetBytes(w.output[0], "call_id").String())
}

func TestGeneralOAIChatCompactionThroughWSBridge(t *testing.T) {
	cfg := rawChatCompletionsTestConfig()
	cfg.JWT.Secret = "summary-secret"
	upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"id":"cc_1","choices":[{"message":{"role":"assistant","content":"Remember RECALL_17"},"finish_reason":"stop"}],"usage":{"prompt_tokens":12,"completion_tokens":5,"total_tokens":17}}`))}}
	svc := &OpenAIGatewayService{cfg: cfg, httpUpstream: upstream}
	account := forceChatResponsesFallbackAccount()
	account.Extra["openai_chat_compaction_enabled"] = true
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest("GET", "/v1/responses", nil)
	var frames [][]byte
	result, err := svc.proxyChatCompletionsWSBridge(context.Background(), c, account, []byte(`{"type":"response.create","model":"DeepSeek-V4.1-Flash","input":[{"role":"user","content":"Remember RECALL_17"},{"type":"compaction_trigger"}]}`), func(p []byte) error { frames = append(frames, bytes.Clone(p)); return nil })
	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, result.OpenAIWSMode)
	require.Equal(t, "/v1/chat/completions", result.UpstreamEndpoint)
	require.Len(t, frames, 4)
	require.Empty(t, rec.Body.String())
	require.NotEmpty(t, result.RequestID)
	require.True(t, result.wsReplayInputExists)
	require.Equal(t, "compaction", gjson.GetBytes(frames[2], "item.type").String())
	require.NotEmpty(t, gjson.GetBytes(frames[2], "item.encrypted_content").String())
	require.Equal(t, "response.completed", gjson.GetBytes(frames[3], "type").String())
}

func TestGeneralOAIChatCompactionRejectsTruncatedSummary(t *testing.T) {
	cfg := rawChatCompletionsTestConfig()
	cfg.JWT.Secret = "summary-secret"
	svc := &OpenAIGatewayService{cfg: cfg}
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
	resp := &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"choices":[{"message":{"content":"partial"},"finish_reason":"length"}]}`))}
	result, err := svc.finishChatCompaction(c, resp, "DeepSeek-V4.1-Flash", "", "", nil, nil, true, time.Now())
	require.Error(t, err)
	require.Nil(t, result)
	require.NotContains(t, rec.Body.String(), "response.completed")
	require.Equal(t, 502, rec.Code)
}

func TestGeneralOAIWSChatToolContinuation(t *testing.T) {
	first := `data: {"id":"cc_first","choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"call_probe","type":"function","function":{"name":"catalog_probe","arguments":"{\"value\":17}"}}]}}]}` + "\n\n" +
		`data: {"id":"cc_first","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":10,"completion_tokens":8,"total_tokens":18}}` + "\n\ndata: [DONE]\n\n"
	second := `data: {"id":"cc_second","choices":[{"index":0,"delta":{"role":"assistant","content":"TOOL_OK_17"}}]}` + "\n\n" +
		`data: {"id":"cc_second","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":20,"completion_tokens":5,"total_tokens":25}}` + "\n\ndata: [DONE]\n\n"
	upstream := &httpUpstreamRecorder{responses: []*http.Response{
		{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(first))},
		{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(second))},
	}}
	cfg := rawChatCompletionsTestConfig()
	cfg.Gateway.OpenAIWS.Enabled = true
	cfg.Gateway.OpenAIWS.APIKeyEnabled = true
	cfg.Gateway.OpenAIWS.ModeRouterV2Enabled = true
	cfg.Gateway.OpenAIWS.ResponsesWebsocketsV2 = true
	cfg.Gateway.OpenAIWS.WriteTimeoutSeconds = 10
	svc := &OpenAIGatewayService{cfg: cfg, httpUpstream: upstream, cache: &stubGatewayCache{}}
	account := forceChatResponsesFallbackAccount()
	account.Concurrency = 1
	account.Extra["openai_apikey_responses_websockets_v2_mode"] = "http_bridge"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := coderws.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()
		_, first, err := conn.Read(r.Context())
		if err != nil {
			return
		}
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = r
		_ = svc.ProxyResponsesWebSocketFromClient(r.Context(), c, conn, account, "sk-test", first, nil)
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, _, err := coderws.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), nil)
	require.NoError(t, err)
	defer conn.CloseNow()
	require.NoError(t, conn.Write(ctx, coderws.MessageText, []byte(`{"type":"response.create","model":"DeepSeek-V4.1-Flash","input":[{"role":"user","content":"call the tool"}],"tools":[{"type":"function","name":"catalog_probe","parameters":{"type":"object","properties":{"value":{"type":"integer"}}}}]}`)))
	readCompleted := func() []byte {
		for {
			_, raw, err := conn.Read(ctx)
			require.NoError(t, err)
			if gjson.GetBytes(raw, "type").String() == "response.completed" {
				return raw
			}
		}
	}
	completed := readCompleted()
	responseID := gjson.GetBytes(completed, "response.id").String()
	require.NotEmpty(t, responseID)
	require.NoError(t, conn.Write(ctx, coderws.MessageText, []byte(`{"type":"response.create","model":"DeepSeek-V4.1-Flash","previous_response_id":"`+responseID+`","input":[{"type":"function_call_output","call_id":"call_probe","output":"TOOL_OK_17"}]}`)))
	completed = readCompleted()
	require.Contains(t, string(completed), "TOOL_OK_17")
	require.NotEmpty(t, gjson.GetBytes(upstream.lastBody, "tools").Array())
	require.Contains(t, string(upstream.lastBody), "call_probe")
	require.Contains(t, string(upstream.lastBody), "call the tool")
}

func TestGeneralOAIWSPrewarmDoesNotRunInference(t *testing.T) {
	svc := &OpenAIGatewayService{}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("GET", "/v1/responses", nil)
	var frames [][]byte
	result, err := svc.proxyChatCompletionsWSBridge(context.Background(), c, forceChatResponsesFallbackAccount(), []byte(`{"type":"response.create","model":"DeepSeek-V4.1-Flash","generate":false,"input":[{"role":"user","content":"Use tools"}]}`), func(p []byte) error { frames = append(frames, bytes.Clone(p)); return nil })
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, frames, 2)
	require.Equal(t, "response.completed", gjson.GetBytes(frames[1], "type").String())
	require.Empty(t, gjson.GetBytes(frames[1], "response.output").Array())
	require.Zero(t, result.Usage.InputTokens)
	require.Zero(t, result.Usage.OutputTokens)
}
