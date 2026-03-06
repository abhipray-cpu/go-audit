package cloner

import (
	"context"
	"fmt"
	"reflect"

	"github.com/abhipray-cpu/go-audit/domain/port"
)

// Compile-time interface check.
var _ port.ClonerPort = (*ReflectCloner)(nil)

// ReflectCloner is the default ClonerPort using reflection-based deep copy.
type ReflectCloner struct{}

// NewReflect returns a ready-to-use ReflectCloner.
func NewReflect() *ReflectCloner { return &ReflectCloner{} }

// Clone returns an independent deep copy of entity.
// The entity must be a pointer to a struct; the returned value is a pointer
// to a freshly allocated struct of the same type.
func (c *ReflectCloner) Clone(_ context.Context, entity any) (any, error) {
	if entity == nil {
		return nil, fmt.Errorf("cloner: entity must not be nil")
	}

	v := reflect.ValueOf(entity)
	cloned := deepCopy(v)
	return cloned.Interface(), nil
}

// deepCopy recursively copies a reflect.Value.
func deepCopy(v reflect.Value) reflect.Value {
	switch v.Kind() {
	case reflect.Ptr:
		if v.IsNil() {
			return reflect.Zero(v.Type())
		}
		cp := reflect.New(v.Type().Elem())
		cp.Elem().Set(deepCopy(v.Elem()))
		return cp

	case reflect.Struct:
		cp := reflect.New(v.Type()).Elem()
		for i := 0; i < v.NumField(); i++ {
			f := cp.Field(i)
			if !f.CanSet() {
				continue // unexported
			}
			f.Set(deepCopy(v.Field(i)))
		}
		return cp

	case reflect.Slice:
		if v.IsNil() {
			return reflect.Zero(v.Type())
		}
		cp := reflect.MakeSlice(v.Type(), v.Len(), v.Cap())
		for i := 0; i < v.Len(); i++ {
			cp.Index(i).Set(deepCopy(v.Index(i)))
		}
		return cp

	case reflect.Map:
		if v.IsNil() {
			return reflect.Zero(v.Type())
		}
		cp := reflect.MakeMapWithSize(v.Type(), v.Len())
		iter := v.MapRange()
		for iter.Next() {
			cpKey := deepCopy(iter.Key())
			cpVal := deepCopy(iter.Value())
			cp.SetMapIndex(cpKey, cpVal)
		}
		return cp

	case reflect.Interface:
		if v.IsNil() {
			return reflect.Zero(v.Type())
		}
		cp := deepCopy(v.Elem())
		out := reflect.New(v.Type()).Elem()
		out.Set(cp)
		return out

	default:
		// Value types (int, string, bool, float, etc.) are copied by value.
		cp := reflect.New(v.Type()).Elem()
		cp.Set(v)
		return cp
	}
}
