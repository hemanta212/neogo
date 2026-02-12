package neogo

import (
	"reflect"
	"strings"
	"unicode"
)

// LocaleSelector controls locale key preference for locale/base synchronization.
type LocaleSelector interface {
	PreferredKeys() []string
}

type staticLocaleSelector []string

func (s staticLocaleSelector) PreferredKeys() []string { return []string(s) }

// LocalesHook returns a marshal hook for locale fields. Locale fields are
// detected by the "Locale" or "Locales" suffix and use the base field name
// by convention (e.g. ContentLocale -> Content).
func LocalesHook() MarshalHook {
	return LocalesHookWithSelector(staticLocaleSelector{"EnUS", "EnAU"})
}

// LocalesHookWithSelector returns a marshal hook that synchronizes fields with
// *Locale/*Locales suffixes using the provided locale preference order.
func LocalesHookWithSelector(selector LocaleSelector) MarshalHook {
	keys := resolveKeys(selector)
	return func(value reflect.Value) error {
		return localesMarshalHook(value, keys)
	}
}

// LocalesUnmarshalHook returns an unmarshal hook for locale fields that can
// extract flat locale keys (e.g. title_enAU) from the raw props map.
func LocalesUnmarshalHook() UnmarshalHook {
	return LocalesUnmarshalHookWithSelector(staticLocaleSelector{"EnUS", "EnAU"})
}

// LocalesUnmarshalHookWithSelector returns an unmarshal hook that populates
// locale struct fields from flat keys in the raw props map and synchronizes
// base/locale fields using the provided preference order.
func LocalesUnmarshalHookWithSelector(selector LocaleSelector) UnmarshalHook {
	keys := resolveKeys(selector)
	return func(from any, to reflect.Value) error {
		return localesUnmarshalHook(from, to, keys)
	}
}

func resolveKeys(selector LocaleSelector) []string {
	keys := []string{"EnUS", "EnAU"}
	if selector != nil && len(selector.PreferredKeys()) > 0 {
		keys = selector.PreferredKeys()
	}
	return keys
}

// localesMarshalHook syncs base → locale before serialization.
func localesMarshalHook(value reflect.Value, preferredKeys []string) error {
	value = unwindValue(value)
	if !value.IsValid() || value.Kind() != reflect.Struct {
		return nil
	}

	valueT := value.Type()
	for i := 0; i < valueT.NumField(); i++ {
		localeField := valueT.Field(i)
		if localeField.PkgPath != "" {
			continue
		}
		baseName, ok := localeBaseName(localeField.Name)
		if !ok {
			continue
		}
		baseField, ok := valueT.FieldByName(baseName)
		if !ok || baseField.PkgPath != "" {
			continue
		}
		localeValue := value.Field(i)
		baseValue := value.FieldByIndex(baseField.Index)
		if !baseValue.CanSet() {
			continue
		}
		if baseValue.Kind() == reflect.Ptr {
			if baseValue.IsNil() {
				if localeValue.IsZero() {
					continue
				}
				baseValue.Set(reflect.New(baseValue.Type().Elem()))
			}
			baseValue = baseValue.Elem()
		}
		if localeValue.Kind() == reflect.Ptr {
			if localeValue.IsNil() {
				if baseValue.IsZero() {
					continue
				}
				localeValue.Set(reflect.New(localeValue.Type().Elem()))
			}
			localeValue = localeValue.Elem()
		}
		if localeValue.Kind() != reflect.Struct {
			continue
		}
		if baseValue.IsZero() {
			if localeValue.IsZero() {
				continue
			}
			if setBaseFromLocale(baseValue, localeValue, preferredKeys) {
				continue
			}
			continue
		}
		if localeValue.IsZero() {
			setLocaleFromBase(baseValue, localeValue, preferredKeys)
			continue
		}
	}
	return nil
}

// localesUnmarshalHook extracts flat locale keys from the raw props map and
// populates locale struct fields, then syncs locale → base using preference order.
func localesUnmarshalHook(from any, to reflect.Value, preferredKeys []string) error {
	to = unwindValue(to)
	if !to.IsValid() || to.Kind() != reflect.Struct {
		return nil
	}

	props, _ := from.(map[string]any)

	toT := to.Type()
	for i := 0; i < toT.NumField(); i++ {
		localeField := toT.Field(i)
		if localeField.PkgPath != "" {
			continue
		}
		baseName, ok := localeBaseName(localeField.Name)
		if !ok {
			continue
		}
		baseField, ok := toT.FieldByName(baseName)
		if !ok || baseField.PkgPath != "" {
			continue
		}
		localeValue := to.Field(i)
		baseValue := to.FieldByIndex(baseField.Index)
		if !baseValue.CanSet() {
			continue
		}

		// Phase 1: Extract flat keys from raw props into locale struct.
		flatKeysFound := false
		if props != nil {
			flatKeysFound = extractFlatLocaleKeys(props, baseName, localeValue, preferredKeys)
		}

		// Phase 2: Sync locale → base (unmarshal direction).
		// Unwrap pointers for base.
		bv := baseValue
		if bv.Kind() == reflect.Ptr {
			if bv.IsNil() {
				lv := localeValue
				if lv.Kind() == reflect.Ptr {
					if lv.IsNil() {
						continue
					}
					lv = lv.Elem()
				}
				if lv.Kind() != reflect.Struct || lv.IsZero() {
					continue
				}
				baseValue.Set(reflect.New(baseValue.Type().Elem()))
			}
			bv = baseValue.Elem()
		}
		// Unwrap pointers for locale.
		lv := localeValue
		if lv.Kind() == reflect.Ptr {
			if lv.IsNil() {
				continue
			}
			lv = lv.Elem()
		}
		if lv.Kind() != reflect.Struct {
			continue
		}
		// If flat keys were extracted, locale is authoritative - always override base.
		if flatKeysFound {
			setBaseFromLocale(bv, lv, preferredKeys)
			continue
		}
		// Otherwise, standard sync: only set base from locale when base is zero.
		if bv.IsZero() {
			if lv.IsZero() {
				continue
			}
			setBaseFromLocale(bv, lv, preferredKeys)
			continue
		}
	}
	return nil
}

// extractFlatLocaleKeys reads flat keys like "title_enAU" from the props map
// and populates the corresponding locale struct fields. Returns true if any
// flat key was found and set.
func extractFlatLocaleKeys(props map[string]any, baseName string, localeValue reflect.Value, preferredKeys []string) bool {
	// Derive the neo4j property prefix: "Title" → "title"
	prefix := lcFirst(baseName)

	// Ensure we can write to the locale struct. Allocate if it's a nil pointer.
	if localeValue.Kind() == reflect.Ptr {
		if localeValue.IsNil() {
			// Only allocate if there's at least one matching flat key in the map.
			if !hasAnyFlatKey(props, prefix, preferredKeys) {
				return false
			}
			localeValue.Set(reflect.New(localeValue.Type().Elem()))
		}
		localeValue = localeValue.Elem()
	}
	if localeValue.Kind() != reflect.Struct {
		return false
	}

	found := false
	localeT := localeValue.Type()
	for j := 0; j < localeT.NumField(); j++ {
		lf := localeT.Field(j)
		if lf.PkgPath != "" {
			continue
		}
		// Map struct field name to flat key: "EnAU" → "title_enAU"
		flatKey := prefix + "_" + lcFirst(lf.Name)
		v, ok := props[flatKey]
		if !ok {
			continue
		}
		field := localeValue.Field(j)
		if !field.CanSet() {
			continue
		}
		if v == nil {
			continue
		}
		rv := reflect.ValueOf(v)
		if rv.Type().AssignableTo(field.Type()) {
			field.Set(rv)
			found = true
		} else if rv.Type().ConvertibleTo(field.Type()) {
			field.Set(rv.Convert(field.Type()))
			found = true
		}
	}
	return found
}

// hasAnyFlatKey checks if any flat locale key exists in the props map.
func hasAnyFlatKey(props map[string]any, prefix string, preferredKeys []string) bool {
	for _, key := range preferredKeys {
		flatKey := prefix + "_" + lcFirst(key)
		if _, ok := props[flatKey]; ok {
			return true
		}
	}
	return false
}

// lcFirst lowercases the first character of a string.
func lcFirst(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	r[0] = unicode.ToLower(r[0])
	return string(r)
}

func setBaseFromLocale(baseValue, localeValue reflect.Value, preferredKeys []string) bool {
	if localeInner, ok := firstPreferredLocaleValue(localeValue, preferredKeys); ok {
		if assignValue(baseValue, localeInner) {
			return true
		}
	}
	for i := 0; i < localeValue.NumField(); i++ {
		localeInner := localeValue.Field(i)
		if !localeInner.IsValid() || localeInner.IsZero() {
			continue
		}
		if assignValue(baseValue, localeInner) {
			return true
		}
	}
	return false
}

func setLocaleFromBase(baseValue, localeValue reflect.Value, preferredKeys []string) bool {
	for _, key := range preferredKeys {
		field := localeValue.FieldByName(key)
		if !field.IsValid() || !field.CanSet() || !field.IsZero() {
			continue
		}
		if assignValue(field, baseValue) {
			return true
		}
	}
	for i := 0; i < localeValue.NumField(); i++ {
		localeInner := localeValue.Field(i)
		if !localeInner.CanSet() || !localeInner.IsZero() {
			continue
		}
		if assignValue(localeInner, baseValue) {
			return true
		}
	}
	return false
}

func firstPreferredLocaleValue(localeValue reflect.Value, preferredKeys []string) (reflect.Value, bool) {
	for _, key := range preferredKeys {
		field := localeValue.FieldByName(key)
		if !field.IsValid() || field.IsZero() {
			continue
		}
		return field, true
	}
	return reflect.Value{}, false
}

func assignValue(dst, src reflect.Value) bool {
	if !dst.CanSet() {
		return false
	}
	if src.Type().AssignableTo(dst.Type()) {
		dst.Set(src)
		return true
	}
	if src.Type().ConvertibleTo(dst.Type()) {
		dst.Set(src.Convert(dst.Type()))
		return true
	}
	return false
}

func localeBaseName(fieldName string) (string, bool) {
	if strings.HasSuffix(fieldName, "Locales") {
		return strings.TrimSuffix(fieldName, "Locales"), true
	}
	if strings.HasSuffix(fieldName, "Locale") {
		return strings.TrimSuffix(fieldName, "Locale"), true
	}
	return "", false
}
