package neogo

import (
	"reflect"
	"strings"
)

// LocaleSelector controls locale key preference for locale/base synchronization.
type LocaleSelector interface {
	PreferredKeys() []string
}

type staticLocaleSelector []string

func (s staticLocaleSelector) PreferredKeys() []string { return []string(s) }

// LocalesHook returns a hook for locale fields. Locale fields are detected by
// the "Locale" or "Locales" suffix and use the base field name by convention
// (e.g. ContentLocale -> Content).
func LocalesHook() Hook {
	return LocalesHookWithSelector(staticLocaleSelector{"EnUS", "EnAU"})
}

// LocalesHookWithSelector returns a hook that synchronizes fields with
// *Locale/*Locales suffixes using the provided locale preference order.
func LocalesHookWithSelector(selector LocaleSelector) Hook {
	keys := []string{"EnUS", "EnAU"}
	if selector != nil && len(selector.PreferredKeys()) > 0 {
		keys = selector.PreferredKeys()
	}
	return func(value reflect.Value) error {
		return localesHook(value, keys)
	}
}

func localesHook(value reflect.Value, preferredKeys []string) error {
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
