package deepcopy

import (
	"fmt"
	. "reflect"
	"unsafe"
)

type copier func(interface{}, map[uintptr]interface{}) (interface{}, error)

var (
	copiers   map[Kind]copier
	primitive map[Kind]struct{}
)

func init() {
	copiers = map[Kind]copier{
		Array:  _array,
		Map:    _map,
		Ptr:    _pointer,
		Slice:  _slice,
		Struct: _struct,
		String: _string,
	}
	primitive = map[Kind]struct{}{
		Bool:       {},
		Int:        {},
		Int8:       {},
		Int16:      {},
		Int32:      {},
		Int64:      {},
		Uint:       {},
		Uint8:      {},
		Uint16:     {},
		Uint32:     {},
		Uint64:     {},
		Uintptr:    {},
		Float32:    {},
		Float64:    {},
		Complex64:  {},
		Complex128: {},
	}
}

// MustAnything does a deep copy and panics on any errors.
func MustAnything(x interface{}) interface{} {
	dc, err := Anything(x)
	if err != nil {
		panic(err)
	}
	return dc
}

// Anything makes a deep copy of whatever gets passed in. It handles pretty much all known Go types
// (with the exception of channels, unsafe pointers, and functions). Note that this is a truly deep
// copy that will work it's way all the way to the leaves of the types--any pointer will be copied,
// any values in any slice or map will be deep copied, etc.
// Note: in order to avoid an infinite loop, we keep track of any pointers that we've run across.
// If we run into that pointer again, we don't make another deep copy of it; we just replace it with
// the copy we've already made. This also ensures that the cloned result is functionally equivalent
// to the original value.
func Anything(x interface{}) (interface{}, error) {
	ptrs := make(map[uintptr]interface{})
	return _anything(x, ptrs)
}

// StructPointerCopy copies x to y.
// NOTICE: Make sure neither x nor y is nil.
func StructPointerCopy(x, y interface{}) error {
	xv, yv := ValueOf(x), ValueOf(y)
	if xv.Kind() != Ptr || yv.Kind() != Ptr {
		return fmt.Errorf("must pass value with kind of Ptr; got %v, %v", xv.Kind(), yv.Kind())
	}
	if xv.IsNil() || yv.IsNil() {
		return fmt.Errorf("must pass not nil value; got %v, %v", x, y)
	}
	if xv.Type() != yv.Type() {
		return fmt.Errorf("must pass same type value; got %v %v", x, y)
	}
	if xv.Elem().Kind() != Struct || yv.Elem().Kind() != Struct {
		return fmt.Errorf("must pass struct kind value; got %v %v", x, y)
	}
	ptrs := make(map[uintptr]interface{})
	ptrs[xv.Pointer()] = y
	return _struct_pointer(x, y, ptrs)
}

func MustString(s string) string {
	buf := []byte(s)
	ns := (*string)(unsafe.Pointer(&buf))
	return *ns
}

func _anything(x interface{}, ptrs map[uintptr]interface{}) (interface{}, error) {
	v := ValueOf(x)
	if !v.IsValid() {
		return x, nil
	}
	if _, ok := primitive[v.Kind()]; ok {
		return x, nil
	}
	if c, ok := copiers[v.Kind()]; ok {
		return c(x, ptrs)
	}
	t := TypeOf(x)
	return nil, fmt.Errorf("unable to make a deep copy of %v (type: %v) - kind %v is not supported", x, t, v.Kind())
}

func _string(x interface{}, ptrs map[uintptr]interface{}) (interface{}, error) {
	if s, ok := x.(string); ok {
		buf := []byte(s)
		ns := (*string)(unsafe.Pointer(&buf))
		return *ns, nil
	} else {
		return nil, fmt.Errorf("must pass a string value; got %v", x)
	}
}

func _slice(x interface{}, ptrs map[uintptr]interface{}) (interface{}, error) {
	v := ValueOf(x)
	if v.Kind() != Slice {
		return nil, fmt.Errorf("must pass a value with kind of Slice; got %v", v.Kind())
	}
	t := TypeOf(x)
	if v.IsNil() {
		return Zero(t).Interface(), nil
	}
	// Create a new slice and, for each item in the slice, make a deep copy of it.
	size := v.Len()
	dc := MakeSlice(t, size, size)
	for i := 0; i < size; i++ {
		item, err := _anything(v.Index(i).Interface(), ptrs)
		if err != nil {
			return nil, fmt.Errorf("failed to clone slice item at index %v: %v", i, err)
		}
		iv := ValueOf(item)
		if iv.IsValid() {
			dc.Index(i).Set(iv)
		}
	}
	return dc.Interface(), nil
}

func _map(x interface{}, ptrs map[uintptr]interface{}) (interface{}, error) {
	v := ValueOf(x)
	if v.Kind() != Map {
		return nil, fmt.Errorf("must pass a value with kind of Map; got %v", v.Kind())
	}
	t := TypeOf(x)
	if v.IsNil() {
		return Zero(t).Interface(), nil
	}
	dc := MakeMapWithSize(t, v.Len())
	iter := v.MapRange()
	for iter.Next() {
		item, err := _anything(iter.Value().Interface(), ptrs)
		if err != nil {
			return nil, fmt.Errorf("failed to clone map item %v: %v", iter.Key().Interface(), err)
		}
		k, err := _anything(iter.Key().Interface(), ptrs)
		if err != nil {
			return nil, fmt.Errorf("failed to clone the map key %v: %v", k, err)
		}
		dc.SetMapIndex(ValueOf(k), ValueOf(item))
	}
	return dc.Interface(), nil
}

func _pointer(x interface{}, ptrs map[uintptr]interface{}) (interface{}, error) {
	v := ValueOf(x)
	if v.Kind() != Ptr {
		return nil, fmt.Errorf("must pass a value with kind of Ptr; got %v", v.Kind())
	}

	t := TypeOf(x)
	if v.IsNil() {
		return Zero(t).Interface(), nil
	}

	addr := v.Pointer()
	if dc, ok := ptrs[addr]; ok {
		return dc, nil
	}
	dc := New(t.Elem())
	ptrs[addr] = dc.Interface()

	switch v.Elem().Kind() {
	case Struct:
		err := _struct_pointer(x, dc.Interface(), ptrs)
		if err != nil {
			return nil, err
		}
		return dc.Interface(), nil
	case Array:
		err := _array_pointer(x, dc.Interface(), ptrs)
		if err != nil {
			return nil, err
		}
		return dc.Interface(), nil
	default:
		item, err := _anything(v.Elem().Interface(), ptrs)
		if err != nil {
			return nil, fmt.Errorf("failed to copy the value under the pointer %v: %v", v, err)
		}
		iv := ValueOf(item)
		if iv.IsValid() {
			dc.Elem().Set(ValueOf(item))
		}
		return dc.Interface(), nil
	}
}

func _struct_pointer(x, y interface{}, ptrs map[uintptr]interface{}) error {
	v := ValueOf(x).Elem()
	t := v.Type()
	dc := ValueOf(y).Elem()
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if f.PkgPath != "" {
			continue
		}
		item, err := _anything(v.Field(i).Interface(), ptrs)
		if err != nil {
			return fmt.Errorf("failed to copy the field %v in the struct %#v: %v", t.Field(i).Name, x, err)
		}
		dc.Field(i).Set(ValueOf(item))
	}
	return nil
}

func _struct(x interface{}, ptrs map[uintptr]interface{}) (interface{}, error) {
	v := ValueOf(x)
	if v.Kind() != Struct {
		return nil, fmt.Errorf("must pass a value with kind of Struct; got %v", v.Kind())
	}
	t := TypeOf(x)
	dc := New(t)
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if f.PkgPath != "" {
			continue
		}
		item, err := _anything(v.Field(i).Interface(), ptrs)
		if err != nil {
			return nil, fmt.Errorf("failed to copy the field %v in the struct %#v: %v", t.Field(i).Name, x, err)
		}
		dc.Elem().Field(i).Set(ValueOf(item))
	}
	return dc.Elem().Interface(), nil
}

func _array(x interface{}, ptrs map[uintptr]interface{}) (interface{}, error) {
	v := ValueOf(x)
	if v.Kind() != Array {
		return nil, fmt.Errorf("must pass a value with kind of Array; got %v", v.Kind())
	}
	t := TypeOf(x)
	size := t.Len()
	dc := New(ArrayOf(size, t.Elem())).Elem()
	for i := 0; i < size; i++ {
		item, err := _anything(v.Index(i).Interface(), ptrs)
		if err != nil {
			return nil, fmt.Errorf("failed to clone array item at index %v: %v", i, err)
		}
		dc.Index(i).Set(ValueOf(item))
	}
	return dc.Interface(), nil
}

func _array_pointer(x, y interface{}, ptrs map[uintptr]interface{}) error {
	v := ValueOf(x).Elem()
	t := v.Type()
	size := t.Len()
	dc := ValueOf(y).Elem()
	for i := 0; i < size; i++ {
		item, err := _anything(v.Index(i).Interface(), ptrs)
		if err != nil {
			return fmt.Errorf("failed to clone array item at index %v: %v", i, err)
		}
		dc.Index(i).Set(ValueOf(item))
	}
	return nil
}
