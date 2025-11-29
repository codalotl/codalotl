package docubot

import (
	"fmt"
	"testing"
)

// Use to review prompts
func TestPrompts(t *testing.T) {
	t.SkipNow()
	fmt.Println(promptAddDocumentation())
	fmt.Println("-----")
	fmt.Println(promptPolish())
	fmt.Println("-----")
	fmt.Println(promptFindErrors())
	fmt.Println("-----")
	fmt.Println(promptIncorperateFeedback())
	fmt.Println("-----")
	fmt.Println(promptChooseBestDocs())
	fmt.Println("-----")
}
