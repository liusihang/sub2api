//go:build unit

package service

import (
	"bytes"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

const testBioPolicyCustomMessage = "该请求因本地生物安全策略被预拒绝，请调整表述或更换Deepseek模型后重试。"

func newBioPolicyWSRewriteTestContext(t *testing.T) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	customMessage := testBioPolicyCustomMessage
	rule := &model.ErrorPassthroughRule{
		ID:              1,
		Name:            "OpenAI 生物安全策略提示",
		Enabled:         true,
		Priority:        10,
		ErrorCodes:      []int{400},
		Keywords:        []string{"bio_policy"},
		MatchMode:       model.MatchModeAll,
		Platforms:       []string{PlatformOpenAI},
		PassthroughCode: true,
		PassthroughBody: false,
		CustomMessage:   &customMessage,
	}
	BindErrorPassthroughService(c, newTestService([]*model.ErrorPassthroughRule{rule}))
	return c
}

func TestRewriteOpenAIWSPassthroughClientPayload_TopLevelError(t *testing.T) {
	c := newBioPolicyWSRewriteTestContext(t)
	payload := []byte(`{"type":"error","sequence_number":1,"error":{"type":"invalid_request_error","code":"bio_policy","message":"This content was flagged for possible biological risk."}}`)

	got := rewriteOpenAIWSPassthroughClientPayload(c, PlatformOpenAI, payload)

	require.Equal(t, "error", gjson.GetBytes(got, "type").String())
	require.Equal(t, "bio_policy", gjson.GetBytes(got, "error.code").String())
	require.Equal(t, "invalid_request_error", gjson.GetBytes(got, "error.type").String())
	require.Equal(t, testBioPolicyCustomMessage, gjson.GetBytes(got, "error.message").String())
	require.Equal(t, int64(1), gjson.GetBytes(got, "sequence_number").Int())
}

func TestRewriteOpenAIWSPassthroughClientPayload_ResponseFailed(t *testing.T) {
	c := newBioPolicyWSRewriteTestContext(t)
	payload := []byte(`{"type":"response.failed","response":{"id":"resp_bio","status":"failed","error":{"type":"invalid_request_error","code":"bio_policy","message":"This content was flagged for possible biological risk."}}}`)

	got := rewriteOpenAIWSPassthroughClientPayload(c, PlatformOpenAI, payload)

	require.Equal(t, "response.failed", gjson.GetBytes(got, "type").String())
	require.Equal(t, "resp_bio", gjson.GetBytes(got, "response.id").String())
	require.Equal(t, "bio_policy", gjson.GetBytes(got, "response.error.code").String())
	require.Equal(t, testBioPolicyCustomMessage, gjson.GetBytes(got, "response.error.message").String())
}

func TestRewriteOpenAIWSPassthroughClientPayload_LeavesOtherErrorsUnchanged(t *testing.T) {
	c := newBioPolicyWSRewriteTestContext(t)
	payload := []byte(`{"type":"error","error":{"type":"invalid_request_error","code":"invalid_request","message":"bad request"}}`)

	got := rewriteOpenAIWSPassthroughClientPayload(c, PlatformOpenAI, payload)

	require.True(t, bytes.Equal(payload, got))
}

func TestRewriteOpenAIWSPassthroughClientPayload_NoBoundRuleService(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	payload := []byte(`{"type":"error","error":{"type":"invalid_request_error","code":"bio_policy","message":"This content was flagged for possible biological risk."}}`)

	got := rewriteOpenAIWSPassthroughClientPayload(c, PlatformOpenAI, payload)

	require.True(t, bytes.Equal(payload, got))
}

func TestRewriteOpenAIWSPassthroughClientPayload_LeavesNonErrorEventsUnchanged(t *testing.T) {
	c := newBioPolicyWSRewriteTestContext(t)
	payload := []byte(`{"type":"response.completed","response":{"id":"resp_bio","error":{"code":"bio_policy","message":"metadata only"}}}`)

	got := rewriteOpenAIWSPassthroughClientPayload(c, PlatformOpenAI, payload)

	require.True(t, bytes.Equal(payload, got))
}
