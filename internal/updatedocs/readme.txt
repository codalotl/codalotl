
Roadmap:
 * Rebuild this to update the AST (possibly multiple times with multiple snippets) and output ONCE with format/print.
   I was running into issues with manipulating the AST -- it's harder than it looks, as you need to diligently keep track of byte offsets for each
   node and their comments, or else comments become dissociated with their nodes and print in unexpected places.
 * Normalize wording around "source" to mean source code?
 * Idea: it may be worth while writing everything to an in-memory store instead of disk to the real file, if there's multiple snippets to apply.
   Besides perhaps being more performant (minor benefit), the real benefit is that it has tighter bounds around partial writes and error conditions.
   For instance, what if we get an error midway through writing comments. Is that okay?
 * Focus on user error messages
   - when field is "Foo, Bar int" but snippet is "Foo int", currently generic type mismatch from shape check, but ideally, say what it is.
   - many more

temporary todo list.
 - i think i need to tackle const blocks, because enums are very common and that's how you do it.
 

uncategorized
 - always print out docs with a newline above it.
 - can document an embedded struct
 - if there's a comment like "func foo() { // comment", where does the comment live in the ast?