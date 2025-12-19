package coretools

import "github.com/codalotl/codalotl/internal/tools/authdomain"

type authorizerReadCall struct {
	RequestPermission bool
	ToolName          string
	Paths             []string
}

type authorizerWriteCall struct {
	RequestPermission bool
	ToolName          string
	Paths             []string
}

type authorizerShellCall struct {
	RequestPermission bool
	Cwd               string
	Command           []string
}

type stubAuthorizer struct {
	readCalls  []authorizerReadCall
	writeCalls []authorizerWriteCall
	shellCalls []authorizerShellCall

	readResp  func(requestPermission bool, requestReason string, toolName string, absPath ...string) error
	writeResp func(requestPermission bool, requestReason string, toolName string, absPath ...string) error
	shellResp func(requestPermission bool, requestReason string, cwd string, command []string) error
}

func (s *stubAuthorizer) SandboxDir() string {
	return ""
}

func (s *stubAuthorizer) CodeUnitDir() string {
	return ""
}

func (s *stubAuthorizer) IsCodeUnitDomain() bool {
	return false
}

func (s *stubAuthorizer) WithoutCodeUnit() authdomain.Authorizer {
	return s
}

func (s *stubAuthorizer) Close() {}

func (s *stubAuthorizer) IsAuthorizedForRead(requestPermission bool, requestReason string, toolName string, absPath ...string) error {
	call := authorizerReadCall{
		RequestPermission: requestPermission,
		ToolName:          toolName,
		Paths:             append([]string(nil), absPath...),
	}
	s.readCalls = append(s.readCalls, call)
	if s.readResp != nil {
		return s.readResp(requestPermission, requestReason, toolName, absPath...)
	}
	return nil
}

func (s *stubAuthorizer) IsAuthorizedForWrite(requestPermission bool, requestReason string, toolName string, absPath ...string) error {
	call := authorizerWriteCall{
		RequestPermission: requestPermission,
		ToolName:          toolName,
		Paths:             append([]string(nil), absPath...),
	}
	s.writeCalls = append(s.writeCalls, call)
	if s.writeResp != nil {
		return s.writeResp(requestPermission, requestReason, toolName, absPath...)
	}
	return nil
}

func (s *stubAuthorizer) IsShellAuthorized(requestPermission bool, requestReason string, cwd string, command []string) error {
	call := authorizerShellCall{
		RequestPermission: requestPermission,
		Cwd:               cwd,
		Command:           append([]string(nil), command...),
	}
	s.shellCalls = append(s.shellCalls, call)
	if s.shellResp != nil {
		return s.shellResp(requestPermission, requestReason, cwd, command)
	}
	return nil
}
