package coretools

type authorizerReadCall struct {
	RequestPermission bool
	ToolName          string
	SandboxDir        string
	Paths             []string
}

type authorizerWriteCall struct {
	RequestPermission bool
	ToolName          string
	SandboxDir        string
	Paths             []string
}

type authorizerShellCall struct {
	RequestPermission bool
	SandboxDir        string
	Cwd               string
	Command           []string
}

type stubAuthorizer struct {
	readCalls  []authorizerReadCall
	writeCalls []authorizerWriteCall
	shellCalls []authorizerShellCall

	readResp  func(requestPermission bool, requestReason string, toolName string, sandboxDir string, absPath ...string) error
	writeResp func(requestPermission bool, requestReason string, toolName string, sandboxDir string, absPath ...string) error
	shellResp func(requestPermission bool, requestReason string, sandboxDir string, cwd string, command []string) error
}

func (s *stubAuthorizer) IsAuthorizedForRead(requestPermission bool, requestReason string, toolName string, sandboxDir string, absPath ...string) error {
	call := authorizerReadCall{
		RequestPermission: requestPermission,
		ToolName:          toolName,
		SandboxDir:        sandboxDir,
		Paths:             append([]string(nil), absPath...),
	}
	s.readCalls = append(s.readCalls, call)
	if s.readResp != nil {
		return s.readResp(requestPermission, requestReason, toolName, sandboxDir, absPath...)
	}
	return nil
}

func (s *stubAuthorizer) IsAuthorizedForWrite(requestPermission bool, requestReason string, toolName string, sandboxDir string, absPath ...string) error {
	call := authorizerWriteCall{
		RequestPermission: requestPermission,
		ToolName:          toolName,
		SandboxDir:        sandboxDir,
		Paths:             append([]string(nil), absPath...),
	}
	s.writeCalls = append(s.writeCalls, call)
	if s.writeResp != nil {
		return s.writeResp(requestPermission, requestReason, toolName, sandboxDir, absPath...)
	}
	return nil
}

func (s *stubAuthorizer) IsShellAuthorized(requestPermission bool, requestReason string, sandboxDir string, cwd string, command []string) error {
	call := authorizerShellCall{
		RequestPermission: requestPermission,
		SandboxDir:        sandboxDir,
		Cwd:               cwd,
		Command:           append([]string(nil), command...),
	}
	s.shellCalls = append(s.shellCalls, call)
	if s.shellResp != nil {
		return s.shellResp(requestPermission, requestReason, sandboxDir, cwd, command)
	}
	return nil
}
