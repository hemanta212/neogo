package neogo

import (
	"fmt"
	"reflect"

	"github.com/rlch/neogo/internal"
)

func PropsFromStruct(value any) (map[string]any, error) {
	v := reflect.ValueOf(value)
	for v.Kind() == reflect.Ptr {
		if v.IsNil() {
			return map[string]any{}, nil
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return nil, fmt.Errorf("props value must be a struct, got %s", v.Kind())
	}

	props := make(map[string]any)
	if err := collectProps(v, "", props); err != nil {
		return nil, err
	}
	return props, nil
}

func collectProps(value reflect.Value, prefix string, props map[string]any) error {
	value = derefAll(value)
	if !value.IsValid() || value.Kind() != reflect.Struct {
		return nil
	}

	valueT := value.Type()
	for i := 0; i < valueT.NumField(); i++ {
		fv := value.Field(i)
		ft := valueT.Field(i)
		if ft.PkgPath != "" {
			continue
		}

		tag, hasTag := internal.PropTagForField(ft)
		if hasTag && tag.Ignore {
			continue
		}
		if !hasTag {
			if ft.Anonymous {
				if err := collectProps(fv, prefix, props); err != nil {
					return err
				}
			}
			continue
		}
		if tag.Flatten {
			if err := internal.ValidateFlattenType(ft.Type); err != nil {
				return err
			}
			if fv.Kind() == reflect.Ptr && fv.IsNil() {
				continue
			}
			if fv.IsZero() {
				continue
			}
			flattenPrefix := tag.Name
			if flattenPrefix == "" {
				flattenPrefix = internal.DefaultPropName(ft.Name)
			}
			if err := collectProps(fv, internal.JoinPrefix(prefix, flattenPrefix), props); err != nil {
				return err
			}
			continue
		}
		if fv.IsZero() {
			continue
		}
		name := tag.Name
		if name == "" {
			name = internal.DefaultPropName(ft.Name)
		}
		key := internal.JoinPrefix(prefix, name)
		if _, exists := props[key]; exists {
			return fmt.Errorf("duplicate property key: %s", key)
		}
		props[key] = fv.Interface()
	}
	return nil
}

func derefAll(value reflect.Value) reflect.Value {
	for value.IsValid() && value.Kind() == reflect.Ptr {
		if value.IsNil() {
			return reflect.Value{}
		}
		value = value.Elem()
	}
	return value
}
