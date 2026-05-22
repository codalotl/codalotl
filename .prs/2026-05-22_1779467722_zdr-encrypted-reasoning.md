# PR

## User Summary (do not modify)

The previous patch was incomplete: .prs/2026-05-20_1779308755_openai-zdr.md

Somehow it was missed that in ZDR mode, we need to re-send encrypted reasoning content. Fix this.

Implementing this is (probably) the easy-ish part. The hard part is validating it works. You (AIs) have a tendancy to just write code, see if tests pass, and call it a day. That won't work here. You must actually test this, with more than a single trivial request, that the real OpenAI actually works. Moreover, validate that input caching is working properly:
- The env has the real openai keys. Use them. Don't worry about sending live traffic to openai.
- make integration tests (see INTEGRATION_TEST) that exercise this.
- Test this with something like `go run . exec` with $CODALOTL_ZDR=true
- make sure caching w/ resending encrypted reasoning fully works.
