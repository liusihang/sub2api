//go:build unit

package service

import (
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"testing"
)

func TestTongjiResponsesIncludeHTTPAndWSBridge(t *testing.T) {
	account := tongjiKimiTestAccount()
	body := []byte(`{"type":"response.create","model":"DeepSeek-V4.1-Flash","include":["reasoning.encrypted_content","output.encrypted_content","message.output_text.logprobs"],"input":[{"type":"reasoning","encrypted_content":"unchanged-sealed-history"}],"metadata":{"id":9007199254740993}}`)
	out, err := normalizeTongjiResponsesInclude(account, body)
	require.NoError(t, err)
	require.JSONEq(t, `["reasoning.encrypted_content","message.output_text.logprobs"]`, gjson.GetBytes(out, "include").Raw)
	require.Equal(t, "unchanged-sealed-history", gjson.GetBytes(out, "input.0.encrypted_content").String())
	require.Equal(t, "9007199254740993", gjson.GetBytes(out, "metadata.id").Raw)
	wsBody, err := prepareOpenAIWSHTTPBridgeBody(account, body)
	require.NoError(t, err)
	require.JSONEq(t, gjson.GetBytes(out, "include").Raw, gjson.GetBytes(wsBody, "include").Raw)
	require.Equal(t, "unchanged-sealed-history", gjson.GetBytes(wsBody, "input.0.encrypted_content").String())
}

func TestTongjiResponsesIncludeScopeAndNoop(t *testing.T) {
	account := tongjiKimiTestAccount()
	body := []byte(`{"include":["output.encrypted_content","output.encrypted_content"]}`)
	out, err := normalizeTongjiResponsesInclude(account, body)
	require.NoError(t, err)
	require.JSONEq(t, `[]`, gjson.GetBytes(out, "include").Raw)
	for _, input := range []string{`{ "include": ["reasoning.encrypted_content"] }`, `{ "input": "test" }`, `{"include":null}`} {
		out, err = normalizeTongjiResponsesInclude(account, []byte(input))
		require.NoError(t, err)
		require.Equal(t, input, string(out))
	}
	for _, base := range []string{"https://api.openai.com/v1", "https://agi.tongji.edu.cn.example.com/v1"} {
		account.Credentials["base_url"] = base
		out, err = normalizeTongjiResponsesInclude(account, body)
		require.NoError(t, err)
		require.Equal(t, body, out)
	}
}
