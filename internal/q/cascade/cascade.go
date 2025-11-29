package cascade

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
)

func computeFieldKey(f reflect.StructField) string {
	// Highest priority: cascade tag name (before first comma). If empty, fall back.
	if tag := f.Tag.Get("cascade"); tag != "" {
		parts := strings.Split(tag, ",")
		name := strings.TrimSpace(parts[0])
		if name == "-" {
			return "-"
		}
		if name != "" {
			return strings.ToLower(name)
		}
	}
	// Next: json tag name; json:"-" should not skip the field, only ignore json naming.
	if tag := f.Tag.Get("json"); tag != "" {
		parts := strings.Split(tag, ",")
		name := strings.TrimSpace(parts[0])
		if name != "" && name != "-" {
			return strings.ToLower(name)
		}
	}
	// Default: field name
	return strings.ToLower(f.Name)
}

func requiredFromCascadeTag(f reflect.StructField) bool {
	tag := f.Tag.Get("cascade")
	if tag == "" {
		return false
	}
	parts := strings.Split(tag, ",")
	for _, p := range parts[1:] {
		if strings.TrimSpace(p) == "required" {
			return true
		}
	}
	return false
}

// Loader builds a prioritized cascade of configuration sources and applies them to a destination struct. Register sources in call order from lowest to highest priority using the With*
// methods, then call StrictlyLoad. The zero value is ready to use; New exists for fluent chaining (ex: New().WithDefaults(...).WithJSONFile(...).WithEnv(...).StrictlyLoad(&cfg)).
type Loader struct {
	sources []cascadeSource // Sources are ordered from low to high priority.
}

type Providence struct {
	SourceType       string // ex: "default", "env", "json_file"
	SourceIdentifier string // ex: "/path/to/file.json". Can be "" for things without identifiers (default map, env).
}

func (p Providence) IsSet() bool {
	return p.SourceType != ""
}

func (p Providence) Default() bool {
	return p.SourceType == "default"
}

// New returns a new Loader ready to register sources and load configuration. It is equivalent to &Loader{} and exists to support fluent chaining.
func New() *Loader {
	return &Loader{}
}

// WithDefaults registers m as the lowest-priority source of default values. Keys may use dot-notation and are matched case-insensitively; values must be allowed normalized types. A
// nil map contributes no values. The method returns the Loader to allow chaining.
func (c *Loader) WithDefaults(m map[string]any) *Loader {
	c.sources = append(c.sources, &sourceMap{isDefaults: true, m: m})
	return c
}

// WithJSONFile registers a JSON file as a source on the Loader.
//
// absolutePath may be absolute or relative and is expanded with ExpandPath. Relative paths are resolved against the current working directory when the file is read. The file is not
// read at call time; any I/O or parse errors occur during loading.
//
// The method returns the Loader to allow chaining.
func (c *Loader) WithJSONFile(absolutePath string) *Loader {
	c.sources = append(c.sources, &sourceJSONFile{path: absolutePath})
	return c
}

// WithNearestJSONFile searches upward from startingAbsolutePath (or, if empty, from the current working directory if it can be determined) for the first readable, non-empty file named
// fileName and adds it as the next-highest-priority source. fileName must be a relative path; it may include directories (ex: "config/app.json"). It panics if fileName is absolute.
// startingAbsolutePath should be an absolute path to a directory or file; if a file path is provided, its directory is used. The search stops at the filesystem root.
//
// The file is not parsed here; any JSON parse errors surface when the sources are loaded later. If no file is found, the loader is unchanged.
//
// The method returns the receiver to allow chaining.
func (c *Loader) WithNearestJSONFile(fileName string, startingAbsolutePath string) *Loader {
	if filepath.IsAbs(fileName) {
		panic("fileName shouldn't be absolute")
	}

	// Determine starting directory:
	start := startingAbsolutePath
	if start == "" {
		if wd, err := os.Getwd(); err == nil {
			start = wd
		}
	}
	if start == "" {
		return c
	}

	// If a file path was provided, use its directory as the starting point.
	if fi, err := os.Stat(start); err == nil && !fi.IsDir() {
		start = filepath.Dir(start)
	}

	// Walk upwards until root.
	for dir := start; ; dir = filepath.Dir(dir) {
		candidate := filepath.Join(dir, fileName)

		if data, err := os.ReadFile(candidate); err == nil {
			if strings.TrimSpace(string(data)) != "" {
				c.sources = append(c.sources, &sourceJSONFile{path: candidate})
				return c
			}
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
	}

	return c
}

// WithEnv registers an environment-variable-backed source. The mapping m associates a configuration key (dots denote nesting) with an environment variable name; missing variables are
// ignored and present values are strings. The source is added with the next higher priority, and the Loader is returned to allow chaining.
func (c *Loader) WithEnv(m map[string]string) *Loader {
	c.sources = append(c.sources, &sourceEnv{envToKey: m})
	return c
}

// StrictlyLoad loads configuration from c's sources into dest, from low to high priority, with later sources overwriting earlier values. dest must be a non- nil pointer to a struct.
//
// Field names are matched case-insensitively (ex: "port" sets field Port). A field tagged with `cascade:",required"` must be set by some source; required fields are validated after
// all sources have been applied.
//
// Values are coerced to the destination field type when reasonable (ex: "4" -> 4 for an int field). If a readable source cannot be parsed or supplies a value that cannot be coerced
// to the field type, StrictlyLoad returns an error; it fails fast and does not continue to later sources to "fix" bad values.
//
// StrictlyLoad does not error when a source is missing or not readable due to permissions, when a source is empty/whitespace-only, or when a source contains unknown keys. Errors from
// individual sources include the source's name for context. On success, dest is populated and StrictlyLoad returns nil.
func (c *Loader) StrictlyLoad(dest any) error {
	// Validate destination is a non-nil pointer to a struct:
	if dest == nil {
		return fmt.Errorf("dest must be a non-nil pointer to struct")
	}
	destVal := reflect.ValueOf(dest)
	if destVal.Kind() != reflect.Ptr || destVal.IsNil() {
		return fmt.Errorf("dest must be a non-nil pointer to struct")
	}
	structVal := reflect.Indirect(destVal)
	if structVal.Kind() != reflect.Struct {
		return fmt.Errorf("dest must be a pointer to struct, got %s", structVal.Kind())
	}

	// Track which required fields were set by any source, using lowercased dot-paths.
	present := map[string]bool{}

	for _, src := range c.sources {
		m, err := src.ToMap()
		if err != nil {
			// Ignore file-not-found and permission errors per contract.
			if errors.Is(err, fs.ErrNotExist) || errors.Is(err, fs.ErrPermission) {
				continue
			}
			return fmt.Errorf("%s: %w", src.Name(), err)
		}
		// Determine provenance for this source
		var prov Providence
		switch s := src.(type) {
		case *sourceMap:
			if s.isDefaults {
				prov = Providence{SourceType: "default"}
			} else {
				prov = Providence{SourceType: "map"}
			}
		case *sourceJSONFile:
			prov = Providence{SourceType: "json_file", SourceIdentifier: ExpandPath(s.path)}
		case *sourceEnv:
			prov = Providence{SourceType: "env"}
		default:
			// Fallback: use Name() so at least something is recorded
			prov = Providence{SourceType: src.Name()}
		}

		if err := applyMapToStruct(structVal, m, "", present, prov); err != nil {
			return fmt.Errorf("%s: %w", src.Name(), err)
		}
	}

	// Validate required fields after all sources are applied.
	if err := validateRequiredFields(structVal, "", present); err != nil {
		return err
	}
	return nil
}

// applyMapToStruct writes values from m into structVal, matching keys to settable struct fields case-insensitively and recursing into nested objects. basePath is a dot-separated prefix
// used in error messages and in the present map, which records lowercase paths for fields that were successfully assigned. Unknown keys are ignored. Values are assigned via setFieldValue,
// which handles pointer allocation, recursion, and type coercion. Returns an error if a provided value has the wrong shape or cannot be coerced to the destination field type. The present
// map must be non-nil. Case-insensitive field name collisions are not supported.
func applyMapToStruct(structVal reflect.Value, m map[string]any, basePath string, present map[string]bool, prov Providence) error {
	structType := structVal.Type()

	// Build case-insensitive index of fields: lower(key) -> index
	// Keys are derived from tags with priority: cascade tag name > json tag name > field name.
	// Return an error when the struct defines fields that collide on the computed key.
	fieldIndex := map[string]int{}
	for i := 0; i < structType.NumField(); i++ {
		f := structType.Field(i)
		if !structVal.Field(i).CanSet() {
			continue
		}
		key := computeFieldKey(f)
		if key == "-" || key == "" {
			continue
		}
		if prevIdx, exists := fieldIndex[key]; exists {
			prevName := structType.Field(prevIdx).Name
			return fmt.Errorf("struct contains case-insensitive field key collision for %q: %s and %s", key, prevName, f.Name)
		}
		fieldIndex[key] = i
	}

	for key, raw := range m {
		keyLower := strings.ToLower(key)
		idx, ok := fieldIndex[keyLower]
		if !ok {
			// Unknown key – ignore
			continue
		}

		fVal := structVal.Field(idx)
		fType := structType.Field(idx)
		childPath := keyLower
		if basePath != "" {
			childPath = basePath + "." + keyLower
		}

		// Prepare a callback to assign the sibling provenance field (if present)
		provIdx, hasProv := fieldIndex[strings.ToLower(fType.Name+"Providence")]
		onAssigned := func() {}
		if hasProv {
			onAssigned = func() {
				pf := structVal.Field(provIdx)
				// Support both value and *Providence field types
				if pf.Kind() == reflect.Ptr {
					if pf.Type().Elem() == reflect.TypeOf(Providence{}) {
						if pf.IsNil() {
							pf.Set(reflect.New(pf.Type().Elem()))
						}
						pf.Elem().Set(reflect.ValueOf(prov))
					}
				} else if pf.Type() == reflect.TypeOf(Providence{}) {
					pf.Set(reflect.ValueOf(prov))
				}
			}
		}

		if err := setFieldValue(fVal, fType, raw, childPath, present, prov, onAssigned); err != nil {
			return err
		}
	}
	return nil
}

// setFieldValue sets fVal from raw, allocating pointer fields as needed and coercing types where reasonable. It records presence at the given path when a concrete value is assigned.
// Supported assignments include:
//   - Struct fields from map[string]any (recursing via applyMapToStruct).
//   - Scalar fields (string, bool, ints, floats) via coerceScalar.
//   - Slices: empty inputs to any slice type; slices of structs from []map[string]any; []string from []string/[]bool/[]int/[]float64; []bool from []bool/[]string; int slices from []int/[]float64/[]string;
//     float slices from []float64/[]int/[]string.
//
// On mismatch of shape or type (after coercion), an error is returned with the offending path. Unsupported destination kinds or slice element kinds also return an error. The present
// map must be non-nil.
func setFieldValue(fVal reflect.Value, fType reflect.StructField, raw any, path string, present map[string]bool, prov Providence, onAssigned func()) error {
	// Handle pointers by allocating as needed
	if fVal.Kind() == reflect.Ptr {
		if fVal.IsNil() {
			fVal.Set(reflect.New(fVal.Type().Elem()))
		}
		return setFieldValue(fVal.Elem(), fType, raw, path, present, prov, onAssigned)
	}

	switch fVal.Kind() {
	case reflect.Struct:
		obj, ok := raw.(map[string]any)
		if !ok {
			return fmt.Errorf("%s: expected object for struct field", path)
		}
		if err := applyMapToStruct(fVal, obj, path, present, prov); err != nil {
			return err
		}
		onAssigned()
		return nil

	case reflect.String, reflect.Bool, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Float32, reflect.Float64:
		coerced, err := coerceScalar(raw, fVal.Kind(), path)
		if err != nil {
			return err
		}
		switch fVal.Kind() {
		case reflect.String:
			fVal.SetString(coerced.(string))
		case reflect.Bool:
			fVal.SetBool(coerced.(bool))
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			fVal.SetInt(coerced.(int64))
		case reflect.Float32, reflect.Float64:
			fVal.SetFloat(coerced.(float64))
		}
		present[path] = true
		onAssigned()
		return nil

	case reflect.Slice:
		// Allow empty arrays to coerce to any destination slice type:
		if rv := reflect.ValueOf(raw); rv.Kind() == reflect.Slice && rv.Len() == 0 {
			fVal.Set(reflect.MakeSlice(fVal.Type(), 0, 0))
			present[path] = true
			onAssigned()
			return nil
		}
		// Determine element kind and coerce element-wise via shared scalar coercion.
		elKind := fVal.Type().Elem().Kind()
		switch elKind {
		case reflect.Struct:
			// Slice of structs from []map[string]any
			objArr, ok := raw.([]map[string]any)
			if !ok {
				return fmt.Errorf("%s: cannot coerce %T to slice of objects", path, raw)
			}
			slice := reflect.MakeSlice(fVal.Type(), len(objArr), len(objArr))
			for i := range objArr {
				if err := applyMapToStruct(slice.Index(i), objArr[i], fmt.Sprintf("%s[%d]", path, i), present, prov); err != nil {
					return err
				}
			}
			fVal.Set(slice)
			present[path] = true
			onAssigned()
			return nil
		case reflect.String:
			switch v := raw.(type) {
			case []string:
				slice := reflect.MakeSlice(fVal.Type(), len(v), len(v))
				for i := range v {
					coerced, err := coerceScalar(v[i], elKind, fmt.Sprintf("%s[%d]", path, i))
					if err != nil {
						return err
					}
					slice.Index(i).SetString(coerced.(string))
				}
				fVal.Set(slice)
				present[path] = true
				onAssigned()
				return nil
			case []bool:
				slice := reflect.MakeSlice(fVal.Type(), len(v), len(v))
				for i := range v {
					coerced, err := coerceScalar(v[i], elKind, fmt.Sprintf("%s[%d]", path, i))
					if err != nil {
						return err
					}
					slice.Index(i).SetString(coerced.(string))
				}
				fVal.Set(slice)
				present[path] = true
				onAssigned()
				return nil
			case []int:
				slice := reflect.MakeSlice(fVal.Type(), len(v), len(v))
				for i := range v {
					coerced, err := coerceScalar(v[i], elKind, fmt.Sprintf("%s[%d]", path, i))
					if err != nil {
						return err
					}
					slice.Index(i).SetString(coerced.(string))
				}
				fVal.Set(slice)
				present[path] = true
				onAssigned()
				return nil
			case []float64:
				slice := reflect.MakeSlice(fVal.Type(), len(v), len(v))
				for i := range v {
					coerced, err := coerceScalar(v[i], elKind, fmt.Sprintf("%s[%d]", path, i))
					if err != nil {
						return err
					}
					slice.Index(i).SetString(coerced.(string))
				}
				fVal.Set(slice)
				present[path] = true
				onAssigned()
				return nil
			default:
				return fmt.Errorf("%s: cannot coerce %T to []string", path, raw)
			}
		case reflect.Bool:
			switch v := raw.(type) {
			case []bool:
				slice := reflect.MakeSlice(fVal.Type(), len(v), len(v))
				for i := range v {
					coerced, err := coerceScalar(v[i], elKind, fmt.Sprintf("%s[%d]", path, i))
					if err != nil {
						return err
					}
					slice.Index(i).SetBool(coerced.(bool))
				}
				fVal.Set(slice)
				present[path] = true
				onAssigned()
				return nil
			case []string:
				slice := reflect.MakeSlice(fVal.Type(), len(v), len(v))
				for i := range v {
					coerced, err := coerceScalar(v[i], elKind, fmt.Sprintf("%s[%d]", path, i))
					if err != nil {
						return err
					}
					slice.Index(i).SetBool(coerced.(bool))
				}
				fVal.Set(slice)
				present[path] = true
				onAssigned()
				return nil
			default:
				return fmt.Errorf("%s: cannot coerce %T to []bool", path, raw)
			}
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			switch v := raw.(type) {
			case []int:
				slice := reflect.MakeSlice(fVal.Type(), len(v), len(v))
				for i := range v {
					coerced, err := coerceScalar(v[i], elKind, fmt.Sprintf("%s[%d]", path, i))
					if err != nil {
						return err
					}
					slice.Index(i).SetInt(coerced.(int64))
				}
				fVal.Set(slice)
				present[path] = true
				onAssigned()
				return nil
			case []float64:
				slice := reflect.MakeSlice(fVal.Type(), len(v), len(v))
				for i := range v {
					coerced, err := coerceScalar(v[i], elKind, fmt.Sprintf("%s[%d]", path, i))
					if err != nil {
						return err
					}
					slice.Index(i).SetInt(coerced.(int64))
				}
				fVal.Set(slice)
				present[path] = true
				onAssigned()
				return nil
			case []string:
				slice := reflect.MakeSlice(fVal.Type(), len(v), len(v))
				for i := range v {
					coerced, err := coerceScalar(v[i], elKind, fmt.Sprintf("%s[%d]", path, i))
					if err != nil {
						return err
					}
					slice.Index(i).SetInt(coerced.(int64))
				}
				fVal.Set(slice)
				present[path] = true
				onAssigned()
				return nil
			default:
				return fmt.Errorf("%s: cannot coerce %T to []int", path, raw)
			}
		case reflect.Float32, reflect.Float64:
			switch v := raw.(type) {
			case []float64:
				slice := reflect.MakeSlice(fVal.Type(), len(v), len(v))
				for i := range v {
					coerced, err := coerceScalar(v[i], elKind, fmt.Sprintf("%s[%d]", path, i))
					if err != nil {
						return err
					}
					slice.Index(i).SetFloat(coerced.(float64))
				}
				fVal.Set(slice)
				present[path] = true
				onAssigned()
				return nil
			case []int:
				slice := reflect.MakeSlice(fVal.Type(), len(v), len(v))
				for i := range v {
					coerced, err := coerceScalar(v[i], elKind, fmt.Sprintf("%s[%d]", path, i))
					if err != nil {
						return err
					}
					slice.Index(i).SetFloat(coerced.(float64))
				}
				fVal.Set(slice)
				present[path] = true
				onAssigned()
				return nil
			case []string:
				slice := reflect.MakeSlice(fVal.Type(), len(v), len(v))
				for i := range v {
					coerced, err := coerceScalar(v[i], elKind, fmt.Sprintf("%s[%d]", path, i))
					if err != nil {
						return err
					}
					slice.Index(i).SetFloat(coerced.(float64))
				}
				fVal.Set(slice)
				present[path] = true
				onAssigned()
				return nil
			default:
				return fmt.Errorf("%s: cannot coerce %T to []float", path, raw)
			}
		default:
			return fmt.Errorf("%s: unsupported slice element type %s", path, elKind)
		}

	default:
		return fmt.Errorf("%s: unsupported field kind %s", path, fVal.Kind())
	}
}

// coerceScalar converts raw into a value assignable to a field of targetKind. It supports target kinds String, Bool, the signed Int kinds, and the Float kinds. For String, raw may
// be a string, int, float64, or bool (formatted via strconv). For Bool, raw may be a bool or a string parseable by strconv.ParseBool; whitespace is trimmed. For Int kinds, raw may
// be an int, a float64 (truncated toward zero), or a base-10 string; whitespace is trimmed. The returned value is int64. For Float kinds, raw may be a float64, an int, or a string
// parseable by strconv.ParseFloat; whitespace is trimmed. The returned value is float64. If conversion fails or the kind is unsupported, an error is returned that includes path (ex:
// "parent.child[2]") to aid diagnostics.
func coerceScalar(raw any, targetKind reflect.Kind, path string) (any, error) {
	switch targetKind {
	case reflect.String:
		switch v := raw.(type) {
		case string:
			return v, nil
		case float64:
			return strconv.FormatFloat(v, 'f', -1, 64), nil
		case int:
			return strconv.Itoa(v), nil
		case bool:
			return strconv.FormatBool(v), nil
		default:
			return nil, fmt.Errorf("%s: cannot coerce %T to string", path, raw)
		}
	case reflect.Bool:
		switch v := raw.(type) {
		case bool:
			return v, nil
		case string:
			parsed, err := strconv.ParseBool(strings.TrimSpace(v))
			if err != nil {
				return nil, fmt.Errorf("%s: cannot parse bool from %q", path, v)
			}
			return parsed, nil
		default:
			return nil, fmt.Errorf("%s: cannot coerce %T to bool", path, raw)
		}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		switch v := raw.(type) {
		case int:
			return int64(v), nil
		case float64:
			return int64(v), nil
		case string:
			parsed, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
			if err != nil {
				return nil, fmt.Errorf("%s: cannot parse int from %q", path, v)
			}
			return parsed, nil
		default:
			return nil, fmt.Errorf("%s: cannot coerce %T to int", path, raw)
		}
	case reflect.Float32, reflect.Float64:
		switch v := raw.(type) {
		case float64:
			return v, nil
		case int:
			return float64(v), nil
		case string:
			parsed, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
			if err != nil {
				return nil, fmt.Errorf("%s: cannot parse float from %q", path, v)
			}
			return parsed, nil
		default:
			return nil, fmt.Errorf("%s: cannot coerce %T to float", path, raw)
		}
	default:
		return nil, fmt.Errorf("%s: unsupported scalar kind %s", path, targetKind)
	}
}

// validateRequiredFields verifies that all fields tagged with cascade:"required" in structVal were set, as recorded in present. Field keys are built from lowercased field names joined
// by dots, with slice indices included (ex: "items[0].name"). It recurses into structs, non-nil pointers to structs, and slices of structs. It returns an error naming the first missing
// required key, or nil if all required fields are present. structVal must be a struct value; basePath carries the path prefix used during recursion.
func validateRequiredFields(structVal reflect.Value, basePath string, present map[string]bool) error {
	structType := structVal.Type()
	for i := 0; i < structType.NumField(); i++ {
		f := structType.Field(i)
		fv := structVal.Field(i)
		nameLower := computeFieldKey(f)
		if nameLower == "-" || nameLower == "" {
			continue
		}
		path := nameLower
		if basePath != "" {
			path = basePath + "." + nameLower
		}

		if requiredFromCascadeTag(f) {
			if !present[path] {
				return fmt.Errorf("missing required key: %s", path)
			}
		}

		// Recurse into nested structs and slices of structs
		switch fv.Kind() {
		case reflect.Ptr:
			if !fv.IsNil() && fv.Elem().Kind() == reflect.Struct {
				if err := validateRequiredFields(fv.Elem(), path, present); err != nil {
					return err
				}
			}
		case reflect.Struct:
			if err := validateRequiredFields(fv, path, present); err != nil {
				return err
			}
		case reflect.Slice:
			if fv.Type().Elem().Kind() == reflect.Struct {
				for j := 0; j < fv.Len(); j++ {
					if err := validateRequiredFields(fv.Index(j), fmt.Sprintf("%s[%d]", path, j), present); err != nil {
						return err
					}
				}
			}
		}
	}
	return nil
}
