# PR

## User Summary (do not modify)

Background: clarify_public_api is used when a package tries to use another package, but needs clarification on how to use the api. My hypothesis is that this represents an opportunity to improve the documentation. So clarify_public_api records the q/a using CAS.

Then, `codalotl docs improve-from-clarify` can be used to consume these files and improve the docs.

Unfortunately, this just doesn't work well. Usually the answers are just kinda written in the docs of the identifier, and it feels really out of place. An amazing human engineer would NEVER document a function like this.

I think one problem is that we use the docubot system to improve docs, but docubot has limited context and agency. To improve symbol X, docubot sends the LLM X and X's in/out nodes. It then possibly only has the liberty of documenting X, or not.

However, I suspect a lot of this documentation is best done in doc.go, or a related type. Basically, the original caller of clarify_public_api didn't have the right mental model.

So, let's do the following:

- Remove `codalotl docs improve-from-clarify`
- add a refactor subcommand: `docs-improve-from-clarify`. Update orchestrator prompt to use this.
- This refactor subcommand doesn't call into docubot. Instead, it just spins up a normal package-mode agent with a custom prompt and the q/as.

