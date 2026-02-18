// Package gocodetesting provides helpers for tests that need temporary Go code. It creates throwaway modules and packages, writes provided files, loads them with
// gocode, and cleans up automatically. WithMultiCode and WithCode set up a module/package from in-memory source (ex: map filenames to contents), ensure a package
// clause when missing, load the resulting package, and pass it to a callback. Failures during setup or loading fail the test and the callback is not invoked. AddPackage
// extends a test module created by WithMultiCode to include additional packages. Dedent strips common indentation from multi-line strings so inline test fixtures
// can be indented with surrounding code.
//
// The temporary module uses the path "mymodule", writes files under "mypkg", targets Go 1.18 (generics enabled), and produces the import path "mymodule/mypkg".
package gocodetesting
