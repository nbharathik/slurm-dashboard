package cli

import (
	"encoding/csv"
	"fmt"
	"io"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/nbharathik/slurm-dashboard/internal/textsafe"
)

// writeCSV writes a slice of report structs as CSV. Columns are the JSON
// field names, in order, so they match --json. Lists are joined with
// spaces; nested objects are left out.
func writeCSV(w io.Writer, rows any) error {
	v := reflect.ValueOf(rows)
	if v.Kind() != reflect.Slice {
		return fmt.Errorf("csv: not a list")
	}
	t := v.Type().Elem()
	type col struct {
		name  string
		index []int
	}
	var cols []col
	var walk func(t reflect.Type, index []int)
	walk = func(t reflect.Type, index []int) {
		for i := range t.NumField() {
			f := t.Field(i)
			idx := append(append([]int{}, index...), i)
			if f.Anonymous && f.Type.Kind() == reflect.Struct {
				walk(f.Type, idx)
				continue
			}
			name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
			if !f.IsExported() || name == "-" || name == "" || !csvScalar(f.Type) {
				continue
			}
			cols = append(cols, col{name, idx})
		}
	}
	walk(t, nil)
	cw := csv.NewWriter(w)
	head := make([]string, len(cols))
	for i, c := range cols {
		head[i] = c.name
	}
	if err := cw.Write(head); err != nil {
		return err
	}
	for i := range v.Len() {
		rec := make([]string, len(cols))
		for j, c := range cols {
			rec[j] = csvValue(v.Index(i).FieldByIndex(c.index))
		}
		if err := cw.Write(rec); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}

var timeType = reflect.TypeFor[time.Time]()

func csvScalar(t reflect.Type) bool {
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	switch {
	case t == timeType:
		return true
	case t.Kind() == reflect.Slice:
		return t.Elem().Kind() == reflect.String
	case t.Kind() == reflect.Struct, t.Kind() == reflect.Map:
		return false
	}
	return true
}

func csvValue(v reflect.Value) string {
	if v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return ""
		}
		v = v.Elem()
	}
	if v.Type() == timeType {
		t := v.Interface().(time.Time) //nolint:forcetypeassert // checked above
		if t.IsZero() {
			return ""
		}
		return t.UTC().Format(time.RFC3339)
	}
	switch v.Kind() {
	case reflect.String:
		return csvText(v.String())
	case reflect.Slice:
		parts := make([]string, v.Len())
		for i := range parts {
			parts[i] = v.Index(i).String()
		}
		return csvText(strings.Join(parts, " "))
	case reflect.Bool:
		return strconv.FormatBool(v.Bool())
	case reflect.Float32, reflect.Float64:
		return strconv.FormatFloat(v.Float(), 'f', -1, 64)
	}
	return fmt.Sprint(v.Interface())
}

func csvText(s string) string { return textsafe.CSVCell(s) }
