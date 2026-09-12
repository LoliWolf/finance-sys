package llm

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestChatStreamAssemblesOnlyFinalContent(t *testing.T) {
	source := ": heartbeat\n\ndata: {\"choices\":[{\"delta\":{\"reasoning_content\":\"private reasoning\"}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{\"content\":\"hello\"},\"finish_reason\":\"stop\"}]}\n\n" +
		"data: {\"choices\":[],\"usage\":{}}\n\ndata: [DONE]\n\n"
	raw, err := readChatStream(strings.NewReader(source))
	require.NoError(t, err)
	require.NotContains(t, string(raw), "private reasoning")
	var result chatCompletionResponse
	require.NoError(t, json.Unmarshal(raw, &result))
	require.Equal(t, "hello", result.Choices[0].Message.Content)
}

func TestChatStreamRejectsTruncationAndErrorEvents(t *testing.T) {
	for _, source := range []string{
		"data: {\"choices\":[{\"delta\":{\"content\":\"partial\"}}]}\n\n",
		"data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"length\"}]}\n\ndata: [DONE]\n\n",
		"data: {\"error\":{\"message\":\"secret\"}}\n\n",
	} {
		result, err := readChatStream(strings.NewReader(source))
		require.Error(t, err)
		require.Nil(t, result)
		require.NotContains(t, err.Error(), "secret")
	}
}
