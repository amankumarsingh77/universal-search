package config

import "reflect"

// merge copies non-zero fields from override onto base, recursing into structs.
// Slice and map fields, when non-nil/non-empty on override, replace base wholesale.
func merge(base, override *Config) {
	mergeStructs(reflect.ValueOf(base).Elem(), reflect.ValueOf(override).Elem())
}

func mergeStructs(dst, src reflect.Value) {
	for i := 0; i < src.NumField(); i++ {
		sf := src.Field(i)
		df := dst.Field(i)
		if !df.CanSet() {
			continue
		}
		switch sf.Kind() {
		case reflect.Struct:
			mergeStructs(df, sf)
		default:
			if !sf.IsZero() {
				df.Set(sf)
			}
		}
	}
}
