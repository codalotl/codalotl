package pkgtools

import (
	"errors"
	"fmt"
	"path"
	"strings"
)

// resolveImportPath rewrites caller input so we can locate the package on disk.
//   - moduleImportPath is the module path declared in go.mod (ex: "github.com/user/project").
//   - packageImportPath is the import path the tool caller supplied (ex: "github.com/user/project/pkg/foo" or just "pkg/foo").
//
// The function returns:
//  1. the fully-qualified import path
//  2. the directory path relative to the module root (slash-separated)
//  3. an error if the import path is outside the module or otherwise invalid
func resolveImportPath(moduleImportPath, packageImportPath string) (string, string, error) {
	if packageImportPath == "" {
		return "", "", fmt.Errorf("import_path %q is invalid", packageImportPath)
	}
	if moduleImportPath == "" {
		return "", "", errors.New("module name required")
	}

	if strings.HasPrefix(packageImportPath, "/") || strings.Contains(packageImportPath, "\\") {
		return "", "", fmt.Errorf("import_path %q is invalid", packageImportPath)
	}

	segments := strings.Split(packageImportPath, "/")
	for _, segment := range segments {
		if segment == "" || segment == "." || segment == ".." {
			return "", "", fmt.Errorf("import_path %q is invalid", packageImportPath)
		}
	}

	if packageImportPath == moduleImportPath {
		return packageImportPath, "", nil
	}
	if relative, ok := strings.CutPrefix(packageImportPath, moduleImportPath+"/"); ok {
		return packageImportPath, relative, nil
	}

	relativePath := strings.Join(segments, "/")
	return path.Join(moduleImportPath, relativePath), relativePath, nil
}
