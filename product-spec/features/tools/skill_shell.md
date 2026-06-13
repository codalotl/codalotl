# `skill_shell`

`skill_shell` is identical to `shell`. However, the LLM is advised and admonished to ONLY use `skill_shell` if a skill file gives permission to use a particular shell command. The purpose of this is to restrict the LLM from running shell commands willy nilly in package mode, which often violate the package access constraints, while also giving skill authors a way to let shell commands be run.
