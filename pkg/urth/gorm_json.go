package urth

import (
	"fmt"
	"strconv"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type sqlOp string

const (
	equals      = sqlOp(" = ")
	notEquals   = sqlOp(" <> ")
	notNull     = sqlOp(" IS NOT NULL ")
	isNull      = sqlOp(" IS NULL ")
	greaterThan = sqlOp(" > ")
	lessThan    = sqlOp(" < ")

	isIn    = sqlOp(" IN ")
	isNotIn = sqlOp(" NOT IN ")
)

// JSONQueryExpression json query expression, implements clause.Expression interface to use as querier
type JSONQueryExpression struct {
	column  string
	keys    []string
	hasKeys bool
	extract bool
	path    string

	keysOp      sqlOp
	op          sqlOp
	equalsValue any

	groupOp     bool
	groupValues []string
}

// JSONQuery query column as json
func JSONQuery(column string) *JSONQueryExpression {
	return &JSONQueryExpression{column: column}
}

// Extract extract json with path
func (jsonQuery *JSONQueryExpression) Extract(path string) *JSONQueryExpression {
	jsonQuery.extract = true
	jsonQuery.path = path
	return jsonQuery
}

// HasKey returns clause.Expression
func (jsonQuery *JSONQueryExpression) HasKey(keys ...string) *JSONQueryExpression {
	jsonQuery.keys = keys
	jsonQuery.hasKeys = true
	jsonQuery.keysOp = notNull
	return jsonQuery
}

func (jsonQuery *JSONQueryExpression) HasNoKey(keys ...string) *JSONQueryExpression {
	jsonQuery.keys = keys
	jsonQuery.hasKeys = true
	jsonQuery.keysOp = isNull
	return jsonQuery
}

func (jsonQuery *JSONQueryExpression) setOp(inOp sqlOp, value any, keys ...string) *JSONQueryExpression {
	jsonQuery.keys = keys
	jsonQuery.op = inOp
	jsonQuery.equalsValue = value
	return jsonQuery
}

// Keys returns clause.Expression
func (jsonQuery *JSONQueryExpression) Equals(value any, keys ...string) *JSONQueryExpression {
	return jsonQuery.setOp(equals, value, keys...)
}

func (jsonQuery *JSONQueryExpression) NotEquals(value any, keys ...string) *JSONQueryExpression {
	return jsonQuery.setOp(notEquals, value, keys...)
}

func (jsonQuery *JSONQueryExpression) GreaterThan(value any, keys ...string) *JSONQueryExpression {
	return jsonQuery.setOp(greaterThan, value, keys...)
}

func (jsonQuery *JSONQueryExpression) LessThan(value any, keys ...string) *JSONQueryExpression {
	return jsonQuery.setOp(lessThan, value, keys...)
}

func (jsonQuery *JSONQueryExpression) KeyIn(key string, values ...string) *JSONQueryExpression {
	jsonQuery.keys = []string{key}
	jsonQuery.op = isIn
	jsonQuery.groupValues = values
	jsonQuery.groupOp = true

	return jsonQuery
}

func (jsonQuery *JSONQueryExpression) KeyNotIn(key string, values ...string) *JSONQueryExpression {
	jsonQuery.keys = []string{key}
	jsonQuery.op = isNotIn
	jsonQuery.groupValues = values
	jsonQuery.groupOp = true

	return jsonQuery
}

const prefixDotless = "$"

func jsonPathKey(key string) string {
	return "\"" + key + "\""
}

func jsonQueryJoin(keys []string) string {
	if len(keys) == 1 {
		return prefixDotless + "." + jsonPathKey(keys[0])
	}

	n := len(prefixDotless) + len(keys)
	for i := 0; i < len(keys); i++ {
		n += len(keys[i]) + 2
	}

	var b strings.Builder
	b.Grow(n)
	b.WriteString(prefixDotless)
	for _, key := range keys {
		b.WriteString(".")
		b.WriteString(jsonPathKey(key))
	}

	return b.String()
}

// Build implements clause.Expression
func (jsonQuery *JSONQueryExpression) Build(builder clause.Builder) {
	stmt, ok := builder.(*gorm.Statement)
	if !ok {
		return
	}

	switch stmt.Dialector.Name() {
	case "mysql", "sqlite":
		switch {
		case jsonQuery.extract:
			builder.WriteString("JSON_EXTRACT(")
			builder.WriteQuoted(jsonQuery.column)
			builder.WriteByte(',')
			builder.AddVar(stmt, prefixDotless+"."+jsonQuery.path)
			builder.WriteString(")")
		case jsonQuery.hasKeys:
			if len(jsonQuery.keys) > 0 {
				builder.WriteString("JSON_EXTRACT(")
				builder.WriteQuoted(jsonQuery.column)
				builder.WriteByte(',')
				builder.AddVar(stmt, jsonQueryJoin(jsonQuery.keys))
				builder.WriteString(")")
				builder.WriteString(string(jsonQuery.keysOp))
			}
		case len(jsonQuery.op) > 0:
			if len(jsonQuery.keys) > 0 {
				builder.WriteString("JSON_EXTRACT(")
				builder.WriteQuoted(jsonQuery.column)
				builder.WriteByte(',')
				builder.AddVar(stmt, jsonQueryJoin(jsonQuery.keys))
				builder.WriteString(")")

				builder.WriteString(string(jsonQuery.op))
				if jsonQuery.groupOp {
					builder.WriteString("(")
					for idx, v := range jsonQuery.groupValues {
						if idx > 0 {
							builder.WriteByte(',')
						}

						stmt.AddVar(builder, v)
					}
					builder.WriteString(")")
				} else {
					if value, ok := jsonQuery.equalsValue.(bool); ok {
						builder.WriteString(strconv.FormatBool(value))
					} else {
						stmt.AddVar(builder, jsonQuery.equalsValue)
					}
				}

			}
		}
	case "postgres":
		switch {
		case jsonQuery.extract:
			builder.WriteString(fmt.Sprintf("json_extract_path_text(%v::json,", stmt.Quote(jsonQuery.column)))
			stmt.AddVar(builder, jsonQuery.path)
			builder.WriteByte(')')
		case jsonQuery.hasKeys:
			if len(jsonQuery.keys) > 0 {
				stmt.WriteQuoted(jsonQuery.column)
				stmt.WriteString("::jsonb")
				for _, key := range jsonQuery.keys[0 : len(jsonQuery.keys)-1] {
					stmt.WriteString(" -> ")
					stmt.AddVar(builder, key)
				}

				stmt.WriteString(" ? ")
				stmt.AddVar(builder, jsonQuery.keys[len(jsonQuery.keys)-1])
			}
		case len(jsonQuery.op) > 0:
			if len(jsonQuery.keys) > 0 {
				builder.WriteString(fmt.Sprintf("json_extract_path_text(%v::json,", stmt.Quote(jsonQuery.column)))

				for idx, key := range jsonQuery.keys {
					if idx > 0 {
						builder.WriteByte(',')
					}
					stmt.AddVar(builder, key)
				}
				builder.WriteString(") ")

				builder.WriteString(string(jsonQuery.op))

				if _, ok := jsonQuery.equalsValue.(string); ok {
					stmt.AddVar(builder, jsonQuery.equalsValue)
				} else {
					stmt.AddVar(builder, fmt.Sprint(jsonQuery.equalsValue))
				}
			}
		}
	}

}
