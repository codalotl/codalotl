package renamebot

import (
	"github.com/codalotl/codalotl/internal/llmcomplete"
	_ "embed"
	"encoding/json"
	"strings"
)

//go:embed prompts/propose_renames.md
var promptProposeRenames string

type ProposedRename struct {
	From    string `json:"from"`
	To      string `json:"to"`
	FuncID  string `json:"func_id"`
	Context string `json:"context"`
	File    string `json:"file"`
}

// askLLMForOrganization queries the LLM to get a package reorganization map.
func askLLMForRenames(ctx string, opts BaseOptions) ([]ProposedRename, error) {

	conv := opts.Conversationalist
	if conv == nil {
		conv = llmcomplete.NewConversationalist()
	}
	conversation := conv.NewConversation(llmcomplete.ModelIDOrDefault(opts.Model), promptProposeRenames)
	conversation.SetLogger(opts.Logger)
	conversation.AddUserMessage(ctx)
	resp, err := conversation.Send()
	if err != nil {
		return nil, opts.LogWrappedErr("reorgbot.askLLMForRenames", err)
	}

	// fmt.Println("RESPONSE:")
	// fmt.Println(resp.Text)
	// fmt.Println("----")

	var ret []ProposedRename
	if err := json.Unmarshal([]byte(strings.TrimSpace(resp.Text)), &ret); err != nil {
		return nil, opts.LogWrappedErr("reorgbot.unmarshal_llm_response", err)
	}
	return ret, nil
}
