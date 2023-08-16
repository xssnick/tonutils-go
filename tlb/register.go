package tlb

import (
	"reflect"
	"strings"
)

var registered = map[string]reflect.Type{}

var magicType = reflect.TypeOf(Magic{})

func Register(typ any) {
	t := reflect.TypeOf(typ)
	magic := t.Field(0)
	if magic.Type != magicType {
		panic("first field is not magic")
	}

	tag := magic.Tag.Get("tlb")
	if !strings.HasPrefix(tag, "#") && !strings.HasPrefix(tag, "$") {
		panic("invalid magic tag")
	}

	registered[t.Name()] = t
}
