package gocode

type EachPackageWithIdentifiersCallback func(pkg *Package, identifiers []string, onlyTests bool) error

// EachPackageWithIdentifiers calls the callback up to three times with (package [either pkg or pkg.TestPackage], ids for that package, if ids are only testing ids).
// The order of callbacks is [primary package no tests, primary package tests, testing package]. If there are no ids for a package, no callback will be made (including
// potentially no callbacks for any package, if there's no matching identifiers).
//
// If identifiers is empty, all of pkg's and pkg.TestPackage's identifiers that match optionsIfEmpty will be used. Otherwise, we'll use identifiers, filtered by
// optionsIfNonempty.
//
// If the callback returns an error, further callbacks stop and the error is returned.
func EachPackageWithIdentifiers(pkg *Package, identifiers []string, optionsIfEmpty FilterIdentifiersOptions, optionsIfNonempty FilterIdentifiersOptions, callback EachPackageWithIdentifiersCallback) error {
	// Determine identifiers to use for the primary package:
	var primaryPkgIds []string
	if len(identifiers) == 0 {
		// Use all identifiers that match options
		primaryPkgIds = pkg.FilterIdentifiers(nil, optionsIfEmpty)
	} else {
		primaryPkgIds = pkg.FilterIdentifiers(identifiers, optionsIfNonempty)
	}

	// Separate test and non-test identifiers for the primary package. Also filters invalid identifiers
	var primaryPkgNonTestIds []string
	var primaryPkgTestIds []string
	for _, id := range primaryPkgIds {
		s := pkg.GetSnippet(id)
		if s == nil {
			continue
		}
		if s.Test() {
			primaryPkgTestIds = append(primaryPkgTestIds, id)
		} else {
			primaryPkgNonTestIds = append(primaryPkgNonTestIds, id)
		}
	}

	// Callbacks for primary package: non-tests first, then tests
	if len(primaryPkgNonTestIds) > 0 {
		if err := callback(pkg, primaryPkgNonTestIds, false); err != nil {
			return err
		}
	}
	if len(primaryPkgTestIds) > 0 {
		if err := callback(pkg, primaryPkgTestIds, true); err != nil {
			return err
		}
	}

	// Handle black-box test package (package foo_test), third in order
	if pkg.TestPackage != nil {
		var testPkgIds []string
		if len(identifiers) == 0 {
			// Use all identifiers for the test package that match options
			testPkgIds = pkg.TestPackage.FilterIdentifiers(nil, optionsIfEmpty)
		} else {
			testPkgIds = pkg.TestPackage.FilterIdentifiers(identifiers, optionsIfNonempty)
		}

		if len(testPkgIds) > 0 {
			if err := callback(pkg.TestPackage, testPkgIds, true); err != nil {
				return err
			}
		}
	}

	return nil
}
