package goclitools

import "os/exec"

// ToolRequirement describes an external tool that should be available in PATH.
//
// InstallHint is optional; when provided, it should be a user-facing hint for how to install the tool (for example: "go install ...@latest").
type ToolRequirement struct {
	Name        string
	InstallHint string
}

// ToolStatus is the result of resolving a ToolRequirement via exec.LookPath.
//
// Path is empty when the tool could not be found.
type ToolStatus struct {
	Name        string
	Path        string
	InstallHint string
}

// CheckTools resolves each required tool using exec.LookPath and returns a status for each requirement in the same order. It never returns an error; callers can
// decide which missing tools are fatal.
func CheckTools(reqs []ToolRequirement) []ToolStatus {
	statuses := make([]ToolStatus, 0, len(reqs))
	for _, r := range reqs {
		st := ToolStatus{
			Name:        r.Name,
			InstallHint: r.InstallHint,
		}
		if r.Name != "" {
			if lp, err := exec.LookPath(r.Name); err == nil && lp != "" {
				st.Path = lp
			}
		}
		statuses = append(statuses, st)
	}
	return statuses
}

// DefaultRequiredTools returns the external tools expected by Codalotl's Go workflows.
func DefaultRequiredTools() []ToolRequirement {
	return []ToolRequirement{
		{Name: "go"},
		{Name: "gopls", InstallHint: "go install golang.org/x/tools/gopls@latest"},
		{Name: "goimports", InstallHint: "go install golang.org/x/tools/cmd/goimports@latest"},
		{Name: "gofmt"},
		{Name: "git"},
	}
}
