package form

import "reflect"

// structValue returns the underlying struct value and type for a submittable.
// It accepts either a struct value or a non-nil pointer to a struct.
func structValue(submittable any) (reflect.Value, reflect.Type, bool) {
	if submittable == nil {
		panic("submittable must not be nil")
	}

	value := reflect.ValueOf(submittable)
	typ := value.Type()
	isPtr := false

	if typ.Kind() == reflect.Pointer {
		if value.IsNil() {
			panic("submittable must not be nil")
		}
		isPtr = true
		value = value.Elem()
		typ = typ.Elem()
	}

	if typ.Kind() != reflect.Struct {
		panic("submittable must be struct or pointer to struct")
	}

	return value, typ, isPtr
}

// cloneStruct returns an addressable copy of the provided struct value.
func cloneStruct(value reflect.Value, typ reflect.Type) reflect.Value {
	copyValue := reflect.New(typ).Elem()
	copyValue.Set(value)
	return copyValue
}
