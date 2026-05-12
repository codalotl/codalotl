Runs shell commands as directed by Skills:
- ONLY use this tool when indicated by a specific Skill (e.g., running a script located in a Skill's dir, or following the directions indicated by a script).
- Do NOT use this tool if you're NOT following guidance from a Skill.
- Accepts argv-style `command`; returns stdout+stderr.
- Output is limited by `max_output_bytes` (default 40000; clamped to 1024..1048576) with head/tail context preserved around an elision marker.
