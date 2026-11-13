package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

const historyTestSeed = `{"role":"user","content":"INITIAL_TASK"}`
const historyTestReasoning = `{"type":"reasoning","id":"rs_history","summary":[],"content":[{"type":"reasoning_text","text":"STAGE_ONE_COMPLETE_NEXT_STAGE_TWO"}]}`
const historyTestProgress = `{"type":"message","id":"msg_history","role":"assistant","phase":"commentary","content":[{"type":"output_text","text":"STAGE_ONE_CONFIRMED"}]}`
const historyTestCall = `{"type":"function_call","id":"fc_history","call_id":"call_history","name":"lookup","arguments":"{}"}`
const historyTestResult = `{"type":"function_call_output","call_id":"call_history","output":"LOOKUP_COMPLETE"}`
const historyTestInsert = `{"role":"user","content":"INSERT_SWITCH_TARGET"}`

func historyTestArray(items ...string) string { return "[" + strings.Join(items, ",") + "]" }
func historyTestSSE(id, output string) *http.Response {
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(fmt.Sprintf("data: {\"type\":\"response.completed\",\"response\":{\"id\":%q,\"status\":\"completed\",\"model\":\"DeepSeek-V4.1-Flash\",\"output\":%s,\"usage\":{\"input_tokens\":1,\"output_tokens\":1}}}\n\n", id, output)))}
}

// Exercise the actual client WS -> HTTP upstream boundary with captured bodies.
func historyTestSession(t *testing.T, responses []*http.Response) (*OpenAIGatewayService, *httpUpstreamRecorder, func(string, string) gjson.Result) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	upstream := &httpUpstreamRecorder{responses: responses}
	cfg := &config.Config{}
	cfg.Security.URLAllowlist.Enabled = false
	cfg.Security.URLAllowlist.AllowInsecureHTTP = true
	cfg.JWT.Secret = "ws-history-test"
	cfg.Gateway.OpenAIWS.Enabled = true
	cfg.Gateway.OpenAIWS.APIKeyEnabled = true
	cfg.Gateway.OpenAIWS.ResponsesWebsocketsV2 = true
	cfg.Gateway.OpenAIWS.HTTPBridgeEnabled = true
	cfg.Gateway.OpenAIWS.HTTPBridgeThresholdBytes = 1
	cfg.Gateway.OpenAIWS.ReadTimeoutSeconds = 5
	cfg.Gateway.OpenAIWS.WriteTimeoutSeconds = 5
	svc := &OpenAIGatewayService{cfg: cfg, httpUpstream: upstream, cache: &stubGatewayCache{}, openaiWSResolver: NewOpenAIWSProtocolResolver(cfg), toolCorrector: NewCodexToolCorrector()}
	account := &Account{ID: 9001, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Concurrency: 1, Status: StatusActive, Schedulable: true, Credentials: map[string]any{"api_key": "test-key", "base_url": "https://relay.example.test/v1"}, Extra: map[string]any{"responses_websockets_v2_enabled": true, "openai_chat_compaction_enabled": true, "openai_responses_mode": "force_responses"}}
	errCh := make(chan error, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := coderws.Accept(w, r, nil)
		if err != nil {
			errCh <- err
			return
		}
		defer conn.CloseNow()
		readCtx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		_, first, err := conn.Read(readCtx)
		cancel()
		if err != nil {
			errCh <- err
			return
		}
		gc, _ := gin.CreateTestContext(httptest.NewRecorder())
		gc.Request = r.Clone(r.Context())
		errCh <- svc.ProxyResponsesWebSocketFromClient(r.Context(), gc, conn, account, "test-key", first, nil)
	}))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	client, _, err := coderws.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), nil)
	cancel()
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = client.Close(coderws.StatusNormalClosure, "done")
		server.Close()
		select {
		case err := <-errCh:
			require.NoError(t, err)
		case <-time.After(5 * time.Second):
			t.Error("WS bridge did not finish")
		}
	})
	client.SetReadLimit(1 << 20)
	turn := func(input, previous string) gjson.Result {
		t.Helper()
		request := map[string]any{"type": "response.create", "model": "DeepSeek-V4.1-Flash", "stream": true, "store": false, "tools": []any{map[string]any{"type": "function", "name": "lookup", "parameters": map[string]any{"type": "object", "properties": map[string]any{}}}}, "input": json.RawMessage(input)}
		if previous != "" {
			request["previous_response_id"] = previous
		}
		raw, err := json.Marshal(request)
		require.NoError(t, err)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		require.NoError(t, client.Write(ctx, coderws.MessageText, raw))
		for {
			_, event, err := client.Read(ctx)
			require.NoError(t, err)
			switch gjson.GetBytes(event, "type").String() {
			case "response.completed":
				return gjson.GetBytes(event, "response")
			case "error", "response.failed":
				t.Fatalf("WS failed: %s", event)
			}
		}
	}
	return svc, upstream, turn
}

func TestOpenAIWSHTTPBridgePreservesCompleteHistory(t *testing.T) {
	for _, tc := range []struct {
		name   string
		output []string
		delta  []string
	}{
		{"ordinary_tool_continuation", []string{historyTestReasoning, historyTestProgress, historyTestCall}, []string{historyTestResult}},
		{"encrypted_reasoning", []string{`{"type":"reasoning","id":"rs_opaque","summary":[],"encrypted_content":"opaque-history-state"}`, historyTestProgress, historyTestCall}, []string{historyTestResult}},
		{"insert_with_tool_result", []string{historyTestReasoning, historyTestProgress, historyTestCall}, []string{historyTestResult, historyTestInsert}},
		{"assistant_only_then_insert", []string{historyTestReasoning, historyTestProgress}, []string{historyTestInsert}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, upstream, turn := historyTestSession(t, []*http.Response{historyTestSSE("resp_first", historyTestArray(tc.output...)), historyTestSSE("resp_second", "[]")})
			turn(historyTestArray(historyTestSeed), "")
			turn(historyTestArray(tc.delta...), "resp_first")
			require.Len(t, upstream.bodies, 2)
			expected := append([]string{historyTestSeed}, tc.output...)
			expected = append(expected, tc.delta...)
			require.JSONEq(t, historyTestArray(expected...), gjson.GetBytes(upstream.bodies[1], "input").Raw)
			require.False(t, gjson.GetBytes(upstream.bodies[1], "previous_response_id").Exists())
		})
	}
}

func TestOpenAIWSHTTPBridgeRepeatedInsertPreservesAcknowledgments(t *testing.T) {
	secondReasoning := strings.ReplaceAll(historyTestReasoning, "rs_history", "rs_second")
	secondProgress := strings.ReplaceAll(historyTestProgress, "msg_history", "msg_second")
	secondCall := strings.ReplaceAll(strings.ReplaceAll(historyTestCall, "fc_history", "fc_second"), "call_history", "call_second")
	secondResult := strings.ReplaceAll(historyTestResult, "call_history", "call_second")
	secondInsert := `{"role":"user","content":"INSERT_KEEP_METADATA"}`
	_, upstream, turn := historyTestSession(t, []*http.Response{
		historyTestSSE("resp_first", historyTestArray(historyTestReasoning, historyTestProgress, historyTestCall)),
		historyTestSSE("resp_second", historyTestArray(secondReasoning, secondProgress, secondCall)),
		historyTestSSE("resp_third", "[]"),
		historyTestSSE("resp_fourth", "[]"),
	})
	turn(historyTestArray(historyTestSeed), "")
	turn(historyTestArray(historyTestResult, historyTestInsert), "resp_first")
	turn(historyTestArray(secondResult, secondInsert), "resp_second")
	expected := historyTestArray(historyTestSeed, historyTestReasoning, historyTestProgress, historyTestCall, historyTestResult, historyTestInsert, secondReasoning, secondProgress, secondCall, secondResult, secondInsert)
	// Clients can also replay the full window. Do not append it a second time.
	turn(expected, "resp_third")
	require.Len(t, upstream.bodies, 4)
	for _, body := range upstream.bodies[2:] {
		require.JSONEq(t, expected, gjson.GetBytes(body, "input").Raw)
	}
}

func TestOpenAIWSHTTPBridgeCompactionThenInsertPreservesHistory(t *testing.T) {
	summaryResponse := &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"id":"cc_summary","choices":[{"message":{"role":"assistant","content":"COMPACTED_TASK_STATE"},"finish_reason":"stop"}],"usage":{"prompt_tokens":2,"completion_tokens":2}}`))}
	svc, upstream, turn := historyTestSession(t, []*http.Response{
		historyTestSSE("resp_before", historyTestArray(historyTestReasoning, historyTestProgress, historyTestCall)),
		summaryResponse,
		historyTestSSE("resp_after", historyTestArray(historyTestReasoning, historyTestProgress, historyTestCall)),
		historyTestSSE("resp_final", "[]"),
	})
	turn(historyTestArray(historyTestSeed), "")
	before := historyTestArray(historyTestSeed, historyTestReasoning, historyTestProgress, historyTestCall, historyTestResult, `{"type":"compaction_trigger"}`)
	compact := turn(before, "resp_before")
	summary := compact.Get("output.0")
	require.Equal(t, "compaction", summary.Get("type").String())
	plain, err := svc.openChatCompaction(summary.Get("encrypted_content").String())
	require.NoError(t, err)
	require.Equal(t, "COMPACTED_TASK_STATE", plain)
	// Compaction starts a new chain with a replacement window, dropping old history.
	turn(historyTestArray(summary.Raw, historyTestInsert), "")
	turn(historyTestArray(historyTestResult, `{"role":"user","content":"INSERT_KEEP_METADATA"}`), "resp_after")
	require.Len(t, upstream.bodies, 4)
	last := upstream.bodies[3]
	expectedSummary := `{"type":"message","role":"user","content":[{"type":"input_text","text":"Conversation summary from earlier turns:\nCOMPACTED_TASK_STATE"}]}`
	require.JSONEq(t, historyTestArray(expectedSummary, historyTestInsert, historyTestReasoning, historyTestProgress, historyTestCall, historyTestResult, `{"role":"user","content":"INSERT_KEEP_METADATA"}`), gjson.GetBytes(last, "input").Raw)
	require.NotContains(t, string(last), "INITIAL_TASK")
	require.NotContains(t, string(last), "compaction_trigger")
}

func TestOpenAIWSReplayCollectorUsesTerminalOrder(t *testing.T) {
	var collector openAIWSToolCallReplayCollector
	// Parallel output items can finish in a different order from response.output.
	for _, item := range []string{historyTestCall, historyTestProgress, historyTestReasoning} {
		collector.AddEvent("response.output_item.done", []byte(`{"item":`+item+`}`))
	}
	final := historyTestArray(historyTestReasoning, historyTestProgress, historyTestCall)
	for _, event := range []string{"response.completed", "response.done"} {
		collector.AddEvent(event, []byte(`{"response":{"output":`+final+`}}`))
	}
	raw, err := json.Marshal(collector.AllItems())
	require.NoError(t, err)
	require.JSONEq(t, final, string(raw))
	require.Len(t, collector.Items(), 1, "native WS tool replay remains separate from full HTTP history")
}

func TestOpenAIResponsesReasoningCleanupDestinationScope(t *testing.T) {
	for _, tc := range []struct {
		name, baseURL string
		preserve      bool
	}{
		{"default_official", "", false},
		{"official", "https://api.openai.com/v1", false},
		{"official_normalized_hostname", "https://API.OPENAI.COM./v1", false},
		{"tongji", "https://agi.tongji.edu.cn/v1", true},
		{"other_compatible_provider", "https://relay.example.test/v1", true},
		{"hostname_suffix_is_not_official", "https://api.openai.com.example.test/v1", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			account := &Account{ID: 9001, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "test-key", "base_url": tc.baseURL}, Extra: map[string]any{"openai_responses_mode": "force_responses"}}
			body := []byte(`{"model":"DeepSeek-V4.1-Flash","store":false,"stream":false,"input":` + historyTestArray(historyTestSeed, historyTestReasoning, historyTestProgress) + `}`)
			normalized, _, err := normalizeOpenAIResponsesWebSocketCompatibilityBody(body, account, false)
			require.NoError(t, err)
			if tc.preserve {
				require.JSONEq(t, string(body), string(normalized))
			} else {
				require.NotContains(t, string(normalized), "STAGE_ONE_COMPLETE_NEXT_STAGE_TWO")
			}
			upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"id":"resp_ok","object":"response","status":"completed","output":[],"usage":{"input_tokens":1,"output_tokens":1}}`))}}
			svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest("POST", "/v1/responses", strings.NewReader(string(body)))
			_, err = svc.Forward(context.Background(), c, account, body)
			require.NoError(t, err)
			require.Equal(t, tc.preserve, strings.Contains(string(upstream.lastBody), "STAGE_ONE_COMPLETE_NEXT_STAGE_TWO"))
		})
	}
	require.True(t, shouldNormalizeOpenAIResponsesReasoningReplay(&Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth}))
}

func TestOpenAIWSReplayCollectorRefreshesTerminalSnapshot(t *testing.T) {
	var collector openAIWSToolCallReplayCollector
	collector.AddEvent("response.output_item.done", []byte(`{"item":{"type":"message","id":"msg_final","role":"assistant","content":[]}}`))
	final := `[{"type":"message","id":"msg_final","role":"assistant","phase":"commentary","content":[{"type":"output_text","text":"FINAL_PROGRESS"}]}]`
	collector.AddEvent("response.completed", []byte(`{"response":{"output":`+final+`}}`))
	raw, err := json.Marshal(collector.AllItems())
	require.NoError(t, err)
	require.JSONEq(t, final, string(raw))
}

func TestOpenAIWSHTTPBridgeFullHistoryWithProviderIDsDoesNotDuplicate(t *testing.T) {
	// Compatible providers need not use OpenAI's rs_/msg_/fc_ ID namespaces.
	reasoning := strings.ReplaceAll(historyTestReasoning, "rs_history", "item_reasoning")
	progress := strings.ReplaceAll(historyTestProgress, "msg_history", "item_progress")
	call := strings.ReplaceAll(historyTestCall, "fc_history", "item_function")
	_, upstream, turn := historyTestSession(t, []*http.Response{
		historyTestSSE("resp_first", historyTestArray(reasoning, progress, call)),
		historyTestSSE("resp_second", "[]"),
		historyTestSSE("resp_third", "[]"),
	})
	turn(historyTestArray(historyTestSeed), "")
	turn(historyTestArray(historyTestResult, historyTestInsert), "resp_first")
	turn(historyTestArray(historyTestSeed, reasoning, progress, call, historyTestResult, historyTestInsert), "resp_second")
	expected := historyTestArray(historyTestSeed,
		strings.ReplaceAll(reasoning, `"id":"item_reasoning",`, ""),
		strings.ReplaceAll(progress, `"id":"item_progress",`, ""),
		strings.ReplaceAll(call, `"id":"item_function",`, ""),
		historyTestResult, historyTestInsert)
	require.Len(t, upstream.bodies, 3)
	for _, body := range upstream.bodies[1:] {
		require.JSONEq(t, expected, gjson.GetBytes(body, "input").Raw)
	}
}

func TestOpenAICompatibleReasoningCleanupRetriesOnlyAfterExplicitRejection(t *testing.T) {
	body := []byte(`{"model":"gpt-5.5","stream":false,"store":true,"input":` + historyTestArray(historyTestSeed, historyTestReasoning, historyTestProgress) + `}`)
	upstream := &httpUpstreamRecorder{responses: []*http.Response{
		newOpenAIRejectedFieldTestResponse(http.StatusBadRequest, `{"error":{"code":"array_above_max_length","message":"Invalid 'input[1].content': array too long. Expected an array with maximum length 0, but got an array with length 1 instead.","param":"input[1].content","type":"invalid_request_error"}}`),
		newOpenAIRejectedFieldTestResponse(http.StatusOK, `{"output":[],"usage":{"input_tokens":1,"output_tokens":1}}`),
	}}
	_, err := newOpenAIRejectedFieldTestService(upstream).Forward(context.Background(), newOpenAIRejectedFieldTestContext(body), newOpenAIRejectedFieldTestAccount(), body)
	require.NoError(t, err)
	require.Len(t, upstream.bodies, 2)
	require.Contains(t, string(upstream.bodies[0]), "STAGE_ONE_COMPLETE_NEXT_STAGE_TWO")
	require.False(t, gjson.GetBytes(upstream.bodies[1], "input.1.content").Exists())
	require.Equal(t, "STAGE_ONE_CONFIRMED", gjson.GetBytes(upstream.bodies[1], "input.2.content.0.text").String())
}
