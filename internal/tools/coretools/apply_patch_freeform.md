Use the apply_patch tool to edit files. This is a FREEFORM tool, so do not wrap the patch in JSON.

EXAMPLE

*** Begin Patch
*** Add File: hello.txt
+Hello world
*** Update File: src/app.py
*** Move to: src/main.py
@@ def greet():
-print("Hi")
+print("Hello, world!")
*** Delete File: obsolete.txt
*** End Patch

TIPS

- Paths are relative to the sandbox
- Do not send line numbers
- Use anchor text (after the `@@ `) to zoom to a location (e.g., a function), then supply 0-3 lines of context (leading ` `), followed by the changes (leading `-` or `+`)
- Use additional anchors, more leading context, or trailing context to disambiguate.