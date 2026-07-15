package schema

import "strings"

// SQLiteJSONPathLiteral returns a SQL string literal containing a SQLite JSON
// path for one object key. The key is quoted at the JSON-path layer and the
// resulting path is quoted independently for SQL. Keeping this in one place
// prevents schema DDL and runtime queries from drifting or interpolating raw
// field fragments.
func SQLiteJSONPathLiteral(field string) string {
	return SQLiteJSONPathLiteralPath([]string{field})
}

// SQLiteJSONPathLiteralPath is the nested-path counterpart used for canonical
// q.field("parent.child") traversal. Each segment is independently JSON-path
// quoted; callers never concatenate a user field name into SQL syntax.
func SQLiteJSONPathLiteralPath(parts []string) string {
	path := "$"
	for _, part := range parts {
		path += `."` + strings.ReplaceAll(strings.ReplaceAll(part, `\`, `\\`), `"`, `\"`) + `"`
	}
	return "'" + strings.ReplaceAll(path, "'", "''") + "'"
}
