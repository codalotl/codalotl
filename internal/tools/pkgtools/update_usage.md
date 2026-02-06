update_usage will update other packages' code based on the provided `instructions` and `paths`.
- The `paths` parameter is an array, where each element may be either a package directory within the sandbox (relative to the sandbox root) or a Go import path.
    - Each package is a downstream dependency that you want to udpate.
- The tool validates that every path belongs to a downstream package that imports the current package. Any path outside those packages will cause an error.
- A new agent is spawned for each downstream package referenced by `paths`. Each agent is told only about the paths you listed for that package plus your `instructions`.
- This agent will have no context OTHER THAN what you provide in `instructions` and the derived target paths.
- Use this tool when:
    - You change the API of the current package and need to update specific downstream packages.
    - You want downstream packages to start using this package, or change the way they use this package.
