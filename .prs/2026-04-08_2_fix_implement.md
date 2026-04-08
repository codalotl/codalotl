# PR

## User Summary (do not modify)

I was trying /orchestrate. It decided it wanted a new package. Neat. So it made a SPEC.md file in a new folder. Great! It can called the implement tool on this "package". But it errored right away, because there were no .go files.

Just like you can enter /package mode in the TUI with a folder that has no .go files (or any files), implement should be callable on an existing folder with no .go files.

Testing:
- In addition to normal go test test cases, make sure you actually try your solution manually by using `go run . exec`, perhaps on some fixture data you create, in a tmp folder.
