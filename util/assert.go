package util

import (
	"fmt"
	"runtime"
	"reflect"
)

func doPanic(stack int) {
	pc, file, line, ok := runtime.Caller(1 + stack)
	if ok {
		fun := runtime.FuncForPC(pc)
		if fun != nil {
			panic(fmt.Sprintf("Assertion failed in function %s on %s:%d", fun.Name(), file, line))
		} else {
			panic(fmt.Sprintf("Assertion failed on %s:%d", file, line))
		}
	} else {
		panic("Assertion failed")
	}
}

func Assert(v bool) {
	if !v {
		doPanic(1)
	}
}

func AssertNoError(err error) {
	if err != nil {
		panic(err)
	}
}

func AssertNotNil(x interface{}) {
	v := reflect.ValueOf(x)
	if v.IsNil() {
		doPanic(1)
	}
}

func CantReachHere() {
	panic("Can't reach here!")
}

