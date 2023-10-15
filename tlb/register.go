package tlb

import (
	"reflect"
	"strings"
)

var registered = map[string]reflect.Type{}

var magicType = reflect.TypeOf(Magic{})

func register(name string, t reflect.Type) {
	magic := t.Field(0)
	if magic.Type != magicType {
		panic("first field is not magic")
	}

	tag := magic.Tag.Get("tlb")
	if !strings.HasPrefix(tag, "#") && !strings.HasPrefix(tag, "$") {
		panic("invalid magic tag")
	}

	registered[name] = t
}

func RegisterWithName(name string, typ any) {
	t := reflect.TypeOf(typ)
	register(name, t)
}

func Register(typ any) {
	t := reflect.TypeOf(typ)
	register(t.Name(), t)
}
